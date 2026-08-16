package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/active"
	"github.com/janekbaraniewski/openusage/internal/core"
)

type activeComputation struct {
	selection active.Selection
	input     active.SelectInput
	byKey     map[string]core.UsageSnapshot
}

// computeActive resolves the current active provider and narrates its quota
// position from daemon telemetry and the current read model.
func (s *Service) computeActive(ctx context.Context) (active.Selection, error) {
	computed, err := s.computeActiveDetails(ctx)
	if err != nil {
		return active.Selection{}, err
	}
	return computed.selection, nil
}

// computeActiveDetails builds the exact selector input used for the public
// active response. The explainer consumes this same input, so diagnostics do
// not drift into a second selection algorithm.
func (s *Service) computeActiveDetails(ctx context.Context) (activeComputation, error) {
	if s == nil || s.store == nil {
		return activeComputation{}, fmt.Errorf("daemon: telemetry store unavailable")
	}

	lastEvents, err := s.store.LastEventTimes(ctx)
	if err != nil {
		return activeComputation{}, fmt.Errorf("daemon: reading last event times: %w", err)
	}
	raw, _, err := s.store.MetaGet(ctx, active.PinMetaKey)
	if err != nil {
		return activeComputation{}, fmt.Errorf("daemon: reading pin state: %w", err)
	}
	pinState, err := active.DecodePinState(raw)
	if err != nil {
		return activeComputation{}, err
	}
	pinnedKey, pinAlive := active.LivePin(pinState, lastEvents)
	if !pinAlive && strings.TrimSpace(pinState.Key) != "" {
		if err := s.store.MetaClearIfValue(ctx, active.PinMetaKey, raw); err != nil {
			return activeComputation{}, fmt.Errorf("daemon: clearing released pin: %w", err)
		}
	}

	req, err := BuildReadModelRequestFromConfig()
	if err != nil {
		return activeComputation{}, fmt.Errorf("daemon: building read-model request: %w", err)
	}
	snapshots, err := s.computeReadModel(ctx, req)
	if err != nil {
		return activeComputation{}, fmt.Errorf("daemon: reading snapshots: %w", err)
	}

	input, byKey := buildActiveSelectionInput(snapshots, lastEvents, pinnedKey)
	if pinnedKey != "" && !activeInputHasCandidate(input, pinnedKey) {
		// A configured provider can disappear independently of telemetry (for
		// example, an account was removed from settings). Treat that as a
		// released pin rather than keeping stale state forever.
		if err := s.store.MetaClearIfValue(ctx, active.PinMetaKey, raw); err != nil {
			return activeComputation{}, fmt.Errorf("daemon: clearing missing pin: %w", err)
		}
		pinnedKey = ""
		input.PinnedKey = ""
	}
	return activeComputation{
		selection: buildActiveSelectionFromInput(input, byKey, s.now().UTC()),
		input:     input,
		byKey:     byKey,
	}, nil
}

func activeInputHasCandidate(input active.SelectInput, key string) bool {
	for _, candidate := range input.Candidates {
		if candidate.Key == key {
			return true
		}
	}
	return false
}

// buildActiveSelection keeps ranking independent from storage and configuration
// loading, making the core daemon decision easy to test and explain later.
func buildActiveSelection(
	snapshots map[string]core.UsageSnapshot,
	lastEvents map[string]time.Time,
	pinnedKey string,
	now time.Time,
) active.Selection {
	input, byKey := buildActiveSelectionInput(snapshots, lastEvents, pinnedKey)
	return buildActiveSelectionFromInput(input, byKey, now)
}

func buildActiveSelectionInput(
	snapshots map[string]core.UsageSnapshot,
	lastEvents map[string]time.Time,
	pinnedKey string,
) (active.SelectInput, map[string]core.UsageSnapshot) {
	candidates := make([]active.Candidate, 0, len(snapshots))
	byKey := make(map[string]core.UsageSnapshot, len(snapshots))
	for _, snap := range snapshots {
		providerID := strings.TrimSpace(snap.ProviderID)
		if providerID == "" {
			continue
		}
		accountID := strings.TrimSpace(snap.AccountID)
		if accountID == "" {
			accountID = "default"
		}
		key := providerID + ":" + accountID
		candidate := active.Candidate{
			Key:        key,
			ProviderID: providerID,
			AccountID:  accountID,
			Display:    active.DisplayName(providerID),
		}
		if ts, ok := lastEvents[key]; ok {
			t := ts
			candidate.LastEventAt = &t
		}
		candidates = append(candidates, candidate)
		byKey[key] = snap
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Key < candidates[j].Key })
	return active.SelectInput{
		Candidates:    candidates,
		PinnedKey:     pinnedKey,
		PriorityOrder: active.DefaultPriorityOrder,
	}, byKey
}

func buildActiveSelectionFromInput(
	input active.SelectInput,
	byKey map[string]core.UsageSnapshot,
	now time.Time,
) active.Selection {
	winner, source, found := active.Select(input)
	if !found {
		return active.Selection{
			Severity: active.SeverityUnknown,
			Status:   "no_data",
		}
	}

	facts := active.BuildFacts(byKey[winner.Key], now)
	label, severity := active.Narrate(facts, now)
	return active.Selection{
		Selected: winner.Key,
		Display:  winner.Display,
		Pinned:   source == "pinned",
		Severity: severity,
		Label:    label,
		Facts:    facts,
		Source:   source,
		Status:   "ok",
	}
}

// setPin stores a pin, or clears it when key is empty.
func (s *Service) setPin(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		if err := s.store.MetaSet(ctx, active.PinMetaKey, ""); err != nil {
			return fmt.Errorf("daemon: clearing pin: %w", err)
		}
		return nil
	}
	encoded, err := (active.PinState{Key: key, PinnedAt: s.now().UTC()}).Encode()
	if err != nil {
		return err
	}
	if err := s.store.MetaSet(ctx, active.PinMetaKey, encoded); err != nil {
		return fmt.Errorf("daemon: storing pin: %w", err)
	}
	return nil
}

func (s *Service) handleActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sel, err := s.computeActive(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sel)
}

func (s *Service) handleActiveExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	computed, err := s.computeActiveDetails(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ActiveExplainResponse{
		Explanation: active.Explain(computed.input),
	})
}

func (s *Service) handleActiveList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	computed, err := s.computeActiveDetails(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status := computed.selection.Status
	if strings.TrimSpace(status) == "" {
		status = "no_data"
	}
	writeJSON(w, http.StatusOK, active.CandidateList{
		Selected:   computed.selection.Selected,
		Pinned:     computed.input.PinnedKey,
		Status:     status,
		Candidates: computed.input.Candidates,
	})
}

func (s *Service) handleActiveDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	computed, err := s.computeActiveDetails(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, buildActiveDetailResponse(computed.selection, computed.byKey, s.now().UTC()))
}

func (s *Service) handleActivePin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req ActivePinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "decode pin request: "+err.Error())
		return
	}
	if err := s.setPin(r.Context(), req.Key); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func buildActiveDetailResponse(
	selection active.Selection,
	byKey map[string]core.UsageSnapshot,
	now time.Time,
) active.DetailResponse {
	response := active.DetailResponse{
		Selection: selection,
		Rows:      make([]active.DetailRow, 0),
		Status:    selection.Status,
	}
	if selection.Status == "" {
		response.Status = "no_data"
	}

	snap, ok := byKey[selection.Selected]
	if !ok {
		return response
	}
	response.Message = snap.Message
	keys := core.SortedStringKeys(snap.Metrics)
	for _, key := range keys {
		if !core.IncludeDetailMetricKey(key) {
			continue
		}
		metric := snap.Metrics[key]
		resetAt := metricResetAt(snap, key, metric)
		response.Rows = append(response.Rows, active.DetailRow{
			Name:      key,
			Display:   formatActiveMetricDisplay(metric, resetAt, now),
			Limit:     metric.Limit,
			Remaining: metric.Remaining,
			Used:      metric.Used,
			Unit:      metric.Unit,
			Window:    metric.Window,
			ResetAt:   resetAt,
		})
	}
	return response
}

func metricResetAt(snap core.UsageSnapshot, name string, metric core.Metric) *time.Time {
	resetKey := strings.TrimSpace(metric.ResetKey)
	if resetKey == "" {
		resetKey = name
	}
	reset, ok := snap.Resets[resetKey]
	if !ok {
		return nil
	}
	reset = reset.UTC()
	return &reset
}

func formatActiveMetricDisplay(metric core.Metric, resetAt *time.Time, now time.Time) string {
	unit := strings.TrimSpace(metric.Unit)
	var value string
	switch {
	case metric.Limit != nil && metric.Used != nil:
		if unit == "%" || strings.EqualFold(unit, "percent") || strings.EqualFold(unit, "percentage") {
			value = fmt.Sprintf("%.0f%% / %.0f%%", *metric.Used, *metric.Limit)
		} else {
			value = formatActiveNumber(*metric.Used) + " / " + formatActiveNumber(*metric.Limit)
		}
	case metric.Limit != nil && metric.Remaining != nil:
		value = formatActiveNumber(*metric.Remaining) + " / " + formatActiveNumber(*metric.Limit) + " left"
	case metric.Used != nil:
		value = formatActiveNumber(*metric.Used)
	case metric.Remaining != nil:
		value = formatActiveNumber(*metric.Remaining) + " left"
	}
	if value == "" {
		return ""
	}
	if unit != "" && unit != "%" && !strings.Contains(value, unit) {
		value += " " + unit
	}
	if window := strings.TrimSpace(metric.Window); window != "" {
		value += " · " + window
	}
	if resetAt != nil {
		resetLabel := resetAt.Local().Format("Jan 2 15:04")
		if !resetAt.After(now) {
			resetLabel = "now"
		}
		value += " · reset " + resetLabel
	}
	return value
}

func formatActiveNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
