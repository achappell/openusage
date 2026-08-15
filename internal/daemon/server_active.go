package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/active"
	"github.com/janekbaraniewski/openusage/internal/core"
)

type activeComputation struct {
	selection active.Selection
	input     active.SelectInput
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
