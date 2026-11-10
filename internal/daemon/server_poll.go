package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type pollProviderResult struct {
	accountID    string
	snapshot     core.UsageSnapshot
	shouldIngest bool
}

func pollSnapshotsForIngest(results []pollProviderResult) map[string]core.UsageSnapshot {
	snapshots := make(map[string]core.UsageSnapshot)
	for _, result := range results {
		if result.shouldIngest {
			snapshots[result.accountID] = result.snapshot
		}
	}
	return snapshots
}

func (s *Service) runPollLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	s.infof("poll_loop_start", "interval=%s", s.cfg.PollInterval)
	s.pollProviders(ctx)
	for {
		select {
		case <-ctx.Done():
			s.infof("poll_loop_stop", "reason=context_done")
			return
		case <-ticker.C:
			s.pollProviders(ctx)
		}
	}
}

func (s *Service) pollProviders(ctx context.Context) {
	if s == nil || s.quotaIngest == nil {
		return
	}
	started := time.Now()

	accounts, modelNorm, err := s.cachedAccountsAndNorm(ctx)
	if err != nil {
		if s.shouldLog("poll_config_warning", 20*time.Second) {
			s.warnf("poll_config_warning", "error=%v", err)
		}
		return
	}
	if len(accounts) == 0 {
		if s.shouldLog("poll_no_accounts", 30*time.Second) {
			s.infof("poll_skipped", "reason=no_enabled_accounts")
		}
		return
	}

	results := make(chan pollProviderResult, len(accounts))
	var wg sync.WaitGroup

	for _, acct := range accounts {
		wg.Add(1)
		go func(account core.AccountConfig) {
			defer wg.Done()

			// Honour shutdown immediately so we don't run a fresh fetch on
			// an account when the parent ctx has already been cancelled.
			// Without this check the per-fetch 8s timeout (below) is the
			// only ceiling on shutdown — N goroutines × 8s on big setups.
			select {
			case <-ctx.Done():
				return
			default:
			}

			provider, ok := s.providerByID[account.Provider]
			if !ok {
				results <- pollProviderResult{
					accountID: account.ID,
					snapshot: core.UsageSnapshot{
						ProviderID: account.Provider,
						AccountID:  account.ID,
						Timestamp:  s.now().UTC(),
						Status:     core.StatusError,
						Message:    fmt.Sprintf("no provider adapter registered for %q (restart/reinstall telemetry daemon if recently added)", account.Provider),
					},
					shouldIngest: true,
				}
				return
			}

			_, hasDetector := provider.(core.ChangeDetector)
			// Claude also has a remote source: use the HTTP backoff cap and
			// do not let unchanged local files suppress scheduled fetches.
			localOnly := hasDetector && account.Provider != "claude_code"

			// Retry a failed ingest before replacing its snapshot with a new fetch.
			s.pollStateMu.Lock()
			state := s.pollState[account.ID]
			if state != nil && state.hasSnap && state.needsIngest {
				result := pollProviderResult{accountID: account.ID, snapshot: state.lastSnap, shouldIngest: true}
				s.pollStateMu.Unlock()
				results <- result
				return
			}
			s.pollStateMu.Unlock()

			// Adaptive backoff: skip providers that are in a backoff window.
			configChanged := s.creditLimitOverrideChanged(account)
			minimum := time.Duration(0)
			if account.Provider == "claude_code" {
				minimum = 30 * time.Second
				configChanged = false // configuration changes cannot bypass the remote rate limit
			}
			if !configChanged && !s.pollScheduler.shouldPoll(account.ID, localOnly, minimum) {
				s.pollStateMu.Lock()
				state := s.pollState[account.ID]
				if state != nil && state.hasSnap {
					result := pollProviderResult{accountID: account.ID, snapshot: state.lastSnap, shouldIngest: state.needsIngest}
					s.pollStateMu.Unlock()
					results <- result
					return
				}
				s.pollStateMu.Unlock()
				// No cached snapshot yet — must fetch.
			}

			// Local-only providers can skip a fetch when their files are unchanged.
			if localOnly {
				if cached := s.skipUnchangedProvider(provider, account); cached != nil {
					s.pollScheduler.RecordPoll(account.ID, false)
					s.pollStateMu.Lock()
					pending := s.pollState[account.ID].needsIngest
					s.pollStateMu.Unlock()
					results <- pollProviderResult{accountID: account.ID, snapshot: *cached, shouldIngest: pending}
					return
				}
			}

			fetchCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()

			snap, fetchErr := provider.Fetch(fetchCtx, account)
			if fetchErr != nil {
				snap = core.UsageSnapshot{
					ProviderID: account.Provider,
					AccountID:  account.ID,
					Timestamp:  s.now().UTC(),
					Status:     core.StatusError,
					Message:    fetchErr.Error(),
				}
			}
			snap = core.NormalizeUsageSnapshotWithConfig(snap, modelNorm)

			// Track whether data actually changed for adaptive backoff.
			changed := s.pollScheduler.SnapshotChanged(account.ID, snap)
			s.pollScheduler.RecordPoll(account.ID, changed)
			s.pollScheduler.RecordOutcome(account.ID, snap)

			// Persist fresh remote observations even when their values are
			// unchanged, and retain pending work after an ingest failure.
			_, observedErr := time.Parse(time.RFC3339Nano, snap.Attributes["usage_api_observed_at"])
			freshClaude := account.Provider == "claude_code" && fetchErr == nil && observedErr == nil
			s.pollStateMu.Lock()
			pending := changed || freshClaude
			var ingestedVersion uint64
			if previous := s.pollState[account.ID]; previous != nil {
				ingestedVersion = previous.ingestedVersion
			}
			s.pollState[account.ID] = &providerPollState{
				lastFetchAt:         s.now(),
				lastSnap:            snap,
				creditLimitOverride: cloneCreditLimitOverride(account.CreditLimitOverride),
				needsIngest:         pending,
				ingestedVersion:     ingestedVersion,
				hasSnap:             true,
			}
			s.pollStateMu.Unlock()

			results <- pollProviderResult{accountID: account.ID, snapshot: snap, shouldIngest: pending}
		}(acct)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	snapshots := make(map[string]core.UsageSnapshot, len(accounts))
	pollResults := make([]pollProviderResult, 0, len(accounts))
	statusCounts := map[core.Status]int{}
	errorCount := 0
	for result := range results {
		pollResults = append(pollResults, result)
		snapshots[result.accountID] = result.snapshot
		statusCounts[result.snapshot.Status]++
		if result.snapshot.Status == core.StatusError {
			errorCount++
		}
	}
	if len(snapshots) == 0 {
		return
	}

	changedSnapshots := pollSnapshotsForIngest(pollResults)
	var ingestErr error
	if len(changedSnapshots) > 0 {
		ingestCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		ingestErr = s.ingestQuotaSnapshots(ingestCtx, changedSnapshots)
		cancel()
	}
	if ingestErr != nil && s.shouldLog("poll_ingest_warning", 10*time.Second) {
		s.warnf("poll_ingest_warning", "error=%v", ingestErr)
	}
	if ingestErr == nil && len(changedSnapshots) > 0 {
		s.pollStateMu.Lock()
		s.markDataIngested()
		ingestedVersion := s.dataVersion.Load()
		for accountID := range changedSnapshots {
			if state := s.pollState[accountID]; state != nil {
				state.needsIngest = false
				if state.lastSnap.ProviderID == "claude_code" {
					state.ingestedVersion = ingestedVersion
				}
			}
		}
		s.pollStateMu.Unlock()
	}

	durationMs := time.Since(started).Milliseconds()
	if ingestErr != nil || errorCount > 0 || s.shouldLog("poll_cycle_info", 45*time.Second) {
		s.infof(
			"poll_cycle",
			"duration_ms=%d accounts=%d snapshots=%d changed=%d status_ok=%d status_auth=%d status_limited=%d status_error=%d status_unknown=%d ingest_error=%t",
			durationMs,
			len(accounts),
			len(snapshots),
			len(changedSnapshots),
			statusCounts[core.StatusOK],
			statusCounts[core.StatusAuth],
			statusCounts[core.StatusLimited],
			statusCounts[core.StatusError],
			statusCounts[core.StatusUnknown],
			ingestErr != nil,
		)
	}
}

// skipUnchangedProvider checks if a provider's data source has changed since the last
// fetch. Returns the cached snapshot if unchanged, nil if a fresh Fetch() is needed.
func (s *Service) skipUnchangedProvider(provider core.UsageProvider, acct core.AccountConfig) *core.UsageSnapshot {
	detector, ok := provider.(core.ChangeDetector)
	if !ok {
		return nil // provider doesn't support change detection, always fetch
	}

	s.pollStateMu.Lock()
	state := s.pollState[acct.ID]
	s.pollStateMu.Unlock()

	if state == nil || !state.hasSnap {
		return nil // no previous fetch, must run
	}
	if !optionalFloatEqual(state.creditLimitOverride, acct.CreditLimitOverride) {
		return nil
	}

	now := s.now()
	if snapshotResetPassed(state.lastSnap, state.lastFetchAt, now) {
		core.Tracef("[poll] %s/%s: forcing refresh because a reset boundary passed after %s", acct.Provider, acct.ID, state.lastFetchAt.Format(time.RFC3339))
		return nil
	}

	changed, err := detector.HasChanged(acct, state.lastFetchAt)
	if err != nil || changed {
		return nil // error or changed — run Fetch()
	}

	core.Tracef("[poll] %s/%s: skipped (no change since %s)", acct.Provider, acct.ID, state.lastFetchAt.Format(time.RFC3339))
	snap := state.lastSnap
	return &snap
}

func (s *Service) creditLimitOverrideChanged(acct core.AccountConfig) bool {
	s.pollStateMu.Lock()
	state := s.pollState[acct.ID]
	s.pollStateMu.Unlock()
	return state != nil && state.hasSnap && !optionalFloatEqual(state.creditLimitOverride, acct.CreditLimitOverride)
}

func cloneCreditLimitOverride(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func optionalFloatEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func snapshotResetPassed(snap core.UsageSnapshot, since, now time.Time) bool {
	if since.IsZero() || len(snap.Resets) == 0 {
		return false
	}
	for _, resetAt := range snap.Resets {
		if resetAt.IsZero() {
			continue
		}
		if resetAt.After(since) && !resetAt.After(now) {
			return true
		}
	}
	return false
}
