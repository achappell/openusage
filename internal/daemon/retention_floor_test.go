package daemon

import (
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func TestFilterReqsOlderThan(t *testing.T) {
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	floor := now.Add(-30 * 24 * time.Hour)

	reqs := []telemetry.IngestRequest{
		{TurnID: "old", OccurredAt: now.Add(-90 * 24 * time.Hour)}, // dropped
		{TurnID: "edge", OccurredAt: floor.Add(time.Minute)},       // kept (just inside)
		{TurnID: "recent", OccurredAt: now.Add(-time.Hour)},        // kept
		{TurnID: "zero"}, // kept (zero == now)
		{TurnID: "older", OccurredAt: now.Add(-31 * 24 * time.Hour)}, // dropped
	}

	kept, dropped := filterReqsOlderThan(reqs, floor)
	if dropped != 2 {
		t.Errorf("dropped = %d, want 2", dropped)
	}
	keptIDs := map[string]bool{}
	for _, r := range kept {
		keptIDs[r.TurnID] = true
	}
	for _, want := range []string{"edge", "recent", "zero"} {
		if !keptIDs[want] {
			t.Errorf("expected %q to be kept", want)
		}
	}
	for _, gone := range []string{"old", "older"} {
		if keptIDs[gone] {
			t.Errorf("expected %q to be dropped", gone)
		}
	}
}
