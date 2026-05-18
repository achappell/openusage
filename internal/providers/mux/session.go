package mux

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// muxSessionUsage mirrors the shape of session-usage.json files Mux writes
// under ~/.mux/sessions/<workspaceId>/session-usage.json. Field names follow
// upstream's camelCase wire format.
type muxSessionUsage struct {
	Version     int                      `json:"version"`
	ByModel     map[string]muxModelUsage `json:"byModel"`
	LastRequest *muxLastRequest          `json:"lastRequest,omitempty"`
}

// muxModelUsage tracks per-bucket token + cost for one model.
type muxModelUsage struct {
	Input       *muxTokenBucket `json:"input,omitempty"`
	Cached      *muxTokenBucket `json:"cached,omitempty"`
	CacheCreate *muxTokenBucket `json:"cacheCreate,omitempty"`
	Output      *muxTokenBucket `json:"output,omitempty"`
	Reasoning   *muxTokenBucket `json:"reasoning,omitempty"`
}

type muxTokenBucket struct {
	Tokens  *int64   `json:"tokens,omitempty"`
	CostUSD *float64 `json:"cost_usd,omitempty"`
}

type muxLastRequest struct {
	Model     string `json:"model,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// muxModelEntry is the flattened representation we emit downstream.
type muxModelEntry struct {
	SessionID   string
	Provider    string
	Model       string
	Input       int64
	Cached      int64 // cache_read
	CacheCreate int64 // cache_write
	Output      int64
	Reasoning   int64
	CostUSD     float64
	HasCost     bool
	Timestamp   time.Time
}

// readMuxSession parses one session-usage.json file and returns flattened
// per-model entries. Returns nil (no error) when the file is missing,
// unreadable, or empty so directory walks can keep going.
func readMuxSession(path string) ([]muxModelEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("mux: reading %s: %w", path, err)
	}

	var usage muxSessionUsage
	if err := json.Unmarshal(data, &usage); err != nil {
		// Malformed JSON is non-fatal; skip the file.
		return nil, nil
	}
	if len(usage.ByModel) == 0 {
		return nil, nil
	}

	// Session ID = parent directory name (the workspace id).
	sessionID := filepath.Base(filepath.Dir(path))

	// Timestamp: prefer lastRequest.timestamp (milliseconds since epoch);
	// fall back to file mtime.
	var ts time.Time
	if usage.LastRequest != nil && usage.LastRequest.Timestamp > 0 {
		ts = time.UnixMilli(usage.LastRequest.Timestamp).UTC()
	} else if info, statErr := os.Stat(path); statErr == nil {
		ts = info.ModTime().UTC()
	}

	out := make([]muxModelEntry, 0, len(usage.ByModel))
	for key, model := range usage.ByModel {
		provider, modelID := splitModelKey(key)

		in := bucketTokens(model.Input)
		cached := bucketTokens(model.Cached)
		cc := bucketTokens(model.CacheCreate)
		out_ := bucketTokens(model.Output)
		reason := bucketTokens(model.Reasoning)

		if in == 0 && cached == 0 && cc == 0 && out_ == 0 && reason == 0 {
			continue
		}

		cost := bucketCost(model.Input) + bucketCost(model.Cached) +
			bucketCost(model.CacheCreate) + bucketCost(model.Output) +
			bucketCost(model.Reasoning)

		entry := muxModelEntry{
			SessionID:   sessionID,
			Provider:    provider,
			Model:       modelID,
			Input:       in,
			Cached:      cached,
			CacheCreate: cc,
			Output:      out_,
			Reasoning:   reason,
			Timestamp:   ts,
		}
		if cost > 0 {
			entry.CostUSD = cost
			entry.HasCost = true
		}
		out = append(out, entry)
	}
	return out, nil
}

// splitModelKey splits Mux's "provider:model" key on the first colon.
// Keys without a colon return ("", key).
func splitModelKey(key string) (provider, model string) {
	idx := strings.IndexByte(key, ':')
	if idx < 0 {
		return "", key
	}
	return key[:idx], key[idx+1:]
}

func bucketTokens(b *muxTokenBucket) int64 {
	if b == nil || b.Tokens == nil {
		return 0
	}
	v := *b.Tokens
	if v < 0 {
		return 0
	}
	return v
}

func bucketCost(b *muxTokenBucket) float64 {
	if b == nil || b.CostUSD == nil {
		return 0
	}
	v := *b.CostUSD
	if v < 0 {
		return 0
	}
	return v
}
