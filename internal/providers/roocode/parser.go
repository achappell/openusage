// Package roocode parses on-disk per-task event logs produced by the Roo
// Code VS Code extension (and by the closely-related Kilo Code extension,
// which shares the same on-disk schema).
//
// The extension writes two files into each task subdirectory under its
// VS Code globalStorage:
//
//   - ui_messages.json — JSON array of UI events; we read entries whose
//     `say` field equals "api_req_started" and parse a nested JSON blob in
//     the `text` field for per-request token/cost numbers.
//   - api_conversation_history.json — the full conversation transcript,
//     including XML-tagged environment metadata that surfaces the active
//     model slug. We extract the last `<model>...</model>` occurrence as
//     the task's most recent model.
//
// The parser is intentionally shared between the Roo Code and Kilo Code
// providers: both extensions emit identical event schemas and only differ
// in where they live on disk (the extension subdirectory name).
package roocode

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// UIMessagesFile is the JSON file name written by Roo Code / Kilo Code for
// per-task UI events.
const UIMessagesFile = "ui_messages.json"

// APIConversationHistoryFile is the JSON file name holding the full
// conversation, including XML-tagged environment_details blocks.
const APIConversationHistoryFile = "api_conversation_history.json"

// rooUIMessage mirrors one entry in ui_messages.json. The schema has
// evolved slightly across extension versions; only the fields we depend
// on are unmarshaled. Unknown fields are ignored.
type rooUIMessage struct {
	EntryType string          `json:"entry_type,omitempty"`
	Say       string          `json:"say,omitempty"`
	Type      string          `json:"type,omitempty"`
	Text      string          `json:"text,omitempty"`
	TS        json.RawMessage `json:"ts,omitempty"`
}

// rooAPIReq is the nested JSON we expect to find inside the `text` field
// of an `api_req_started` UI message.
type rooAPIReq struct {
	Cost        json.Number `json:"cost,omitempty"`
	TokensIn    json.Number `json:"tokensIn,omitempty"`
	TokensOut   json.Number `json:"tokensOut,omitempty"`
	CacheReads  json.Number `json:"cacheReads,omitempty"`
	CacheWrites json.Number `json:"cacheWrites,omitempty"`
	APIProtocol string      `json:"apiProtocol,omitempty"`
	Request     string      `json:"request,omitempty"`
}

// APICall is a single parsed `api_req_started` event with normalised numeric
// fields. Cost and token counts are clamped at zero so downstream
// aggregation never multiplies in spurious negatives.
type APICall struct {
	TaskID      string
	Timestamp   time.Time
	Cost        float64
	TokensIn    int64
	TokensOut   int64
	CacheReads  int64
	CacheWrites int64
	APIProtocol string // raw `apiProtocol` value
	Provider    string // top-level provider derived from APIProtocol (split on "/")
	Model       string // resolved from api_conversation_history.json (may be empty)
	Client      string // either ClientRooCode or ClientKiloCode
}

// TaskEvent is the per-task aggregation returned by ParseTaskDir.
type TaskEvent struct {
	TaskID   string
	Model    string
	Provider string
	Calls    []APICall
}

// Client identifiers used as `client` dimensions on parsed APICalls so
// downstream consumers (provider Fetch + dedup) can attribute usage back
// to the originating VS Code extension.
const (
	ClientRooCode  = "roocode"
	ClientKiloCode = "kilocode"
)

// Sentinel error so callers can quietly skip tasks without a ui_messages.json.
var errNoUIMessages = fmt.Errorf("roocode: ui_messages.json not present")

// IsNoUIMessages reports whether the error matches the "task directory
// has no ui_messages.json" sentinel. Provider Fetch loops use this to
// silently skip incomplete tasks instead of surfacing them as errors.
func IsNoUIMessages(err error) bool {
	return err == errNoUIMessages
}

// ParseTaskDir reads the two on-disk files for a single task directory and
// returns the aggregated TaskEvent. The clientID is attached to each parsed
// APICall as the `client` field so providers downstream can attribute usage
// back to the originating extension (Roo Code vs Kilo Code).
//
// Returns errNoUIMessages when ui_messages.json is missing; callers should
// treat that as "task not ready" rather than a fatal error.
func ParseTaskDir(taskDir, clientID string) (*TaskEvent, error) {
	taskID := filepath.Base(strings.TrimRight(taskDir, string(filepath.Separator)))

	uiPath := filepath.Join(taskDir, UIMessagesFile)
	uiBytes, err := os.ReadFile(uiPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errNoUIMessages
		}
		return nil, fmt.Errorf("roocode: reading %s: %w", uiPath, err)
	}

	calls, err := parseUIMessages(uiBytes)
	if err != nil {
		return nil, fmt.Errorf("roocode: parsing %s: %w", uiPath, err)
	}

	historyPath := filepath.Join(taskDir, APIConversationHistoryFile)
	model := readLastModelFromHistory(historyPath)

	provider := dominantProvider(calls)
	for i := range calls {
		calls[i].TaskID = taskID
		calls[i].Model = model
		calls[i].Client = clientID
	}

	return &TaskEvent{
		TaskID:   taskID,
		Model:    model,
		Provider: provider,
		Calls:    calls,
	}, nil
}

// parseUIMessages unmarshals a ui_messages.json byte slice and returns one
// APICall per `api_req_started` entry. Malformed entries (top-level or
// nested) are silently skipped so a single corrupt event can't poison the
// entire task.
func parseUIMessages(raw []byte) ([]APICall, error) {
	raw = trimUTF8BOM(raw)
	if len(raw) == 0 {
		return nil, nil
	}

	var messages []rooUIMessage
	if err := json.Unmarshal(raw, &messages); err != nil {
		return nil, fmt.Errorf("decoding ui_messages array: %w", err)
	}

	calls := make([]APICall, 0, len(messages))
	for _, m := range messages {
		if !isAPIReqStarted(m) {
			continue
		}
		text := strings.TrimSpace(m.Text)
		if text == "" {
			continue
		}
		var req rooAPIReq
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		if err := dec.Decode(&req); err != nil {
			// Roo has occasionally shipped non-JSON `text` payloads for
			// non-api_req_started rows; skip silently.
			continue
		}

		call := APICall{
			Timestamp:   parseTimestamp(m.TS),
			Cost:        clampNonNegativeFloat(numberToFloat(req.Cost)),
			TokensIn:    clampNonNegativeInt(numberToInt(req.TokensIn)),
			TokensOut:   clampNonNegativeInt(numberToInt(req.TokensOut)),
			CacheReads:  clampNonNegativeInt(numberToInt(req.CacheReads)),
			CacheWrites: clampNonNegativeInt(numberToInt(req.CacheWrites)),
			APIProtocol: strings.TrimSpace(req.APIProtocol),
		}
		call.Provider = providerFromProtocol(call.APIProtocol)
		calls = append(calls, call)
	}
	return calls, nil
}

// isAPIReqStarted matches Roo's `say == "api_req_started"` filter and is
// tolerant of historical schema variants where the discriminator landed in
// `entry_type` or `type` rather than `say`.
func isAPIReqStarted(m rooUIMessage) bool {
	const target = "api_req_started"
	if strings.EqualFold(strings.TrimSpace(m.Say), target) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(m.EntryType), target) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(m.Type), target) {
		return true
	}
	return false
}

// parseTimestamp accepts the two `ts` serialisations we have observed in
// the wild: a numeric Unix ms value (most common), or an RFC3339 string.
// Returns the zero time on failure; callers either skip zero-stamped rows
// or treat them as undated.
func parseTimestamp(raw json.RawMessage) time.Time {
	trimmed := bytesTrimSpace(raw)
	if len(trimmed) == 0 {
		return time.Time{}
	}
	// String form: `"2025-05-18T..."`.
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err == nil {
			for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
				if t, err := time.Parse(layout, s); err == nil {
					return t.UTC()
				}
			}
		}
		return time.Time{}
	}
	// Numeric form (could be int or float, ms or s — we treat ms when the
	// value is large enough to plausibly be milliseconds, otherwise s).
	var num json.Number
	if err := json.Unmarshal(trimmed, &num); err != nil {
		return time.Time{}
	}
	asFloat, err := num.Float64()
	if err != nil {
		return time.Time{}
	}
	if math.IsNaN(asFloat) || math.IsInf(asFloat, 0) || asFloat <= 0 {
		return time.Time{}
	}
	// Plausibility heuristic: a 2001+ timestamp in seconds fits in 32 bits,
	// while ms requires 41 bits. Anything > 1e12 is therefore ms.
	if asFloat > 1e12 {
		secs := int64(asFloat / 1000)
		ns := int64(math.Mod(asFloat, 1000) * 1e6)
		return time.Unix(secs, ns).UTC()
	}
	secs := int64(asFloat)
	ns := int64(math.Mod(asFloat, 1) * 1e9)
	return time.Unix(secs, ns).UTC()
}

func bytesTrimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\n' || b[0] == '\r') {
		b = b[1:]
	}
	for len(b) > 0 {
		last := b[len(b)-1]
		if last != ' ' && last != '\t' && last != '\n' && last != '\r' {
			break
		}
		b = b[:len(b)-1]
	}
	return b
}

func numberToFloat(n json.Number) float64 {
	if n == "" {
		return 0
	}
	v, err := n.Float64()
	if err != nil {
		// Fall back to a permissive parse so locale-style commas don't
		// nuke the whole row. Anything still unparseable is treated as 0.
		stripped := strings.ReplaceAll(string(n), ",", "")
		if f, e := strconv.ParseFloat(stripped, 64); e == nil {
			return f
		}
		return 0
	}
	return v
}

func numberToInt(n json.Number) int64 {
	if n == "" {
		return 0
	}
	if v, err := n.Int64(); err == nil {
		return v
	}
	// Roo occasionally emits token counts as floats (e.g. 12.0); fall back
	// to a float parse and truncate.
	if v, err := n.Float64(); err == nil {
		return int64(v)
	}
	return 0
}

func clampNonNegativeFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return v
}

func clampNonNegativeInt(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

// providerFromProtocol extracts the top-level provider name from a Roo
// `apiProtocol` value. Roo emits values like "anthropic" or compound forms
// like "bedrock/anthropic"; we keep the leading segment.
func providerFromProtocol(protocol string) string {
	protocol = strings.TrimSpace(protocol)
	if protocol == "" {
		return ""
	}
	if idx := strings.IndexAny(protocol, "/:"); idx > 0 {
		return strings.TrimSpace(protocol[:idx])
	}
	return protocol
}

// dominantProvider returns the most common Provider value across calls. We
// expose this as the task-level provider so the UI can attribute a task to
// a single backend even when calls cross providers mid-session.
func dominantProvider(calls []APICall) string {
	if len(calls) == 0 {
		return ""
	}
	tally := make(map[string]int, 4)
	var best string
	var bestCount int
	for _, c := range calls {
		p := strings.TrimSpace(c.Provider)
		if p == "" {
			continue
		}
		tally[p]++
		if tally[p] > bestCount {
			best = p
			bestCount = tally[p]
		}
	}
	return best
}

// modelTagRE matches `<model>...</model>` blocks anywhere in the
// conversation history file. We deliberately use a non-greedy match so a
// single regex compile handles XML payloads with nested attribute-only
// tags. `(?s)` lets `.` cross newlines for multi-line model entries.
var modelTagRE = regexp.MustCompile(`(?s)<model>\s*(.*?)\s*</model>`)

// slugTagRE matches `<slug>...</slug>` as a fallback when no `<model>` tag
// is present in the conversation history.
var slugTagRE = regexp.MustCompile(`(?s)<slug>\s*(.*?)\s*</slug>`)

// nameTagRE matches `<name>...</name>` as a final fallback.
var nameTagRE = regexp.MustCompile(`(?s)<name>\s*(.*?)\s*</name>`)

// readLastModelFromHistory extracts the last `<model>` tag value from
// api_conversation_history.json. We prefer the last occurrence because
// conversations may switch models mid-task and the most recent value is
// the most representative.
//
// When the file is missing or unreadable we return "" silently — the
// calling provider will record the task without a model attribution
// rather than failing.
func readLastModelFromHistory(historyPath string) string {
	data, err := os.ReadFile(historyPath)
	if err != nil {
		return ""
	}
	// The file is a JSON array, but `<environment_details>` blocks are
	// embedded as plain text inside conversation message strings. The XML
	// regex is correct against the literal file bytes — we don't need to
	// JSON-decode first.
	if model := lastMatch(modelTagRE, data); model != "" {
		return model
	}
	if model := lastMatch(slugTagRE, data); model != "" {
		return model
	}
	if model := lastMatch(nameTagRE, data); model != "" {
		return model
	}
	return ""
}

// lastMatch returns the last capture-group-1 match of re against data,
// trimmed of whitespace. Returns "" when no match is found.
func lastMatch(re *regexp.Regexp, data []byte) string {
	all := re.FindAllSubmatch(data, -1)
	if len(all) == 0 {
		return ""
	}
	last := all[len(all)-1]
	if len(last) < 2 {
		return ""
	}
	return strings.TrimSpace(string(last[1]))
}

// trimUTF8BOM strips the UTF-8 byte-order mark some Windows-written JSON
// files include. json.Unmarshal rejects leading BOMs.
func trimUTF8BOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}
