package roocode

import (
	"sort"
	"time"
)

// Dedup returns the input APICall slice with duplicate (task_id, timestamp)
// rows folded to a single entry. We use a (task_id, ts-unix-nanos) tuple
// because the same physical task directory can appear under multiple VS
// Code variants when a user has Roo Code installed in both Code and
// Cursor — those variants surface identical entries we don't want to
// double-count.
//
// Among duplicates we keep the entry with the highest token + cost sum,
// reasoning that any zero-padded duplicate is the loser of an interrupted
// write rather than the "real" event.
func Dedup(calls []APICall) []APICall {
	if len(calls) <= 1 {
		return calls
	}

	type key struct {
		taskID string
		nsec   int64
	}

	type holder struct {
		idx   int
		score float64
	}

	byKey := make(map[key]holder, len(calls))
	for i, c := range calls {
		// Calls without timestamps fall back to the call's text-only
		// fingerprint via APIProtocol+Model, which is "good enough" for the
		// rare event where Roo emitted no ts at all. We separate these into
		// their own bucket so they don't collide with timestamped rows.
		k := key{taskID: c.TaskID, nsec: c.Timestamp.UTC().UnixNano()}
		if c.Timestamp.IsZero() {
			// Mix in i so each undated call gets a unique bucket.
			k.nsec = int64(-1 - i)
		}
		s := score(c)
		if existing, ok := byKey[k]; ok {
			if s <= existing.score {
				continue
			}
		}
		byKey[k] = holder{idx: i, score: s}
	}

	out := make([]APICall, 0, len(byKey))
	for _, h := range byKey {
		out = append(out, calls[h.idx])
	}

	// Stable chronological order so downstream consumers (DailySeries
	// builders, snapshot dumpers) produce deterministic output. We tie-break
	// on TaskID then APIProtocol so two events at the exact same instant
	// sort consistently across runs.
	sort.SliceStable(out, func(i, j int) bool {
		switch {
		case !out[i].Timestamp.Equal(out[j].Timestamp):
			return out[i].Timestamp.Before(out[j].Timestamp)
		case out[i].TaskID != out[j].TaskID:
			return out[i].TaskID < out[j].TaskID
		default:
			return out[i].APIProtocol < out[j].APIProtocol
		}
	})
	return out
}

// score is the "is this call more complete than its duplicate?" metric.
// We sum the cost and all token counts so a row with non-zero numbers
// always wins over a row that was written first but is missing fields.
func score(c APICall) float64 {
	return c.Cost +
		float64(c.TokensIn) +
		float64(c.TokensOut) +
		float64(c.CacheReads) +
		float64(c.CacheWrites)
}

// CallsAfter returns the subset of calls whose Timestamp is strictly
// after `since`. Calls with zero timestamps are excluded — we can't
// safely date them and including them inflates "recent" windows.
func CallsAfter(calls []APICall, since time.Time) []APICall {
	if since.IsZero() {
		return calls
	}
	out := calls[:0:0]
	for _, c := range calls {
		if c.Timestamp.IsZero() {
			continue
		}
		if c.Timestamp.After(since) {
			out = append(out, c)
		}
	}
	return out
}
