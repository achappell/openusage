package claude_code

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type conversationRecord struct {
	lineNumber int
	timestamp  time.Time
	model      string
	usage      *jsonlUsage
	requestID  string
	messageID  string
	sessionID  string
	cwd        string
	sourcePath string
	content    []jsonlContent
	agentID    string
}

func parseConversationRecords(path string) []conversationRecord {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Resolve the agent label once per file. Detection touches sidecar
	// metadata and (optionally) the parent transcript, so we want to amortise
	// the I/O across every record in the file.
	agentLabel := detectAgentType(path)

	var records []conversationRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry jsonlEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Type != "assistant" || entry.Message == nil {
			continue
		}
		ts, ok := parseJSONLTimestamp(entry.Timestamp)
		if !ok {
			continue
		}
		model := entry.Message.Model
		if model == "" {
			model = "unknown"
		}
		records = append(records, conversationRecord{
			lineNumber: lineNumber,
			timestamp:  ts,
			model:      model,
			usage:      entry.Message.Usage,
			requestID:  entry.RequestID,
			messageID:  entry.Message.ID,
			sessionID:  entry.SessionID,
			cwd:        entry.CWD,
			sourcePath: path,
			content:    entry.Message.Content,
			agentID:    agentLabel,
		})
	}
	return mergeStreamingDuplicates(records)
}

// mergeStreamingDuplicates collapses records that share a non-empty
// `messageId:requestId` composite key into a single record whose token fields
// are the per-field MAX across the duplicates. Claude Code streams partial
// usage records during a turn; in the absence of this merge we either
// undercount (first-wins) or double-count (sum). MAX preserves the final
// totals reported for the message.
//
// Records without enough information to form a composite key are passed
// through unchanged; downstream block-level dedup still applies.
func mergeStreamingDuplicates(records []conversationRecord) []conversationRecord {
	if len(records) < 2 {
		return records
	}
	// Track the slot for each composite key so we can mutate the existing
	// record in place while preserving the original ordering.
	indexByKey := make(map[string]int, len(records))
	out := records[:0]
	for _, rec := range records {
		key := streamingMergeKey(rec)
		if key == "" || rec.usage == nil {
			out = append(out, rec)
			continue
		}
		if existingIdx, ok := indexByKey[key]; ok {
			mergeUsageMax(out[existingIdx].usage, rec.usage)
			// Prefer the earliest timestamp so block boundaries stay
			// stable; everything else (content, source path) stays with
			// the first record we encountered.
			if rec.timestamp.Before(out[existingIdx].timestamp) {
				out[existingIdx].timestamp = rec.timestamp
			}
			continue
		}
		indexByKey[key] = len(out)
		out = append(out, rec)
	}
	return out
}

func streamingMergeKey(r conversationRecord) string {
	if r.messageID == "" && r.requestID == "" {
		return ""
	}
	return r.messageID + ":" + r.requestID
}

func mergeUsageMax(dst, src *jsonlUsage) {
	if dst == nil || src == nil {
		return
	}
	dst.InputTokens = maxInt(dst.InputTokens, src.InputTokens)
	dst.OutputTokens = maxInt(dst.OutputTokens, src.OutputTokens)
	dst.CacheReadInputTokens = maxInt(dst.CacheReadInputTokens, src.CacheReadInputTokens)
	dst.CacheCreationInputTokens = maxInt(dst.CacheCreationInputTokens, src.CacheCreationInputTokens)
	dst.ReasoningTokens = maxInt(dst.ReasoningTokens, src.ReasoningTokens)

	if src.CacheCreation != nil {
		if dst.CacheCreation == nil {
			copy := *src.CacheCreation
			dst.CacheCreation = &copy
		} else {
			dst.CacheCreation.Ephemeral5mInputTokens = maxInt(dst.CacheCreation.Ephemeral5mInputTokens, src.CacheCreation.Ephemeral5mInputTokens)
			dst.CacheCreation.Ephemeral1hInputTokens = maxInt(dst.CacheCreation.Ephemeral1hInputTokens, src.CacheCreation.Ephemeral1hInputTokens)
		}
	}
	if src.ServerToolUse != nil {
		if dst.ServerToolUse == nil {
			copy := *src.ServerToolUse
			dst.ServerToolUse = &copy
		} else {
			dst.ServerToolUse.WebSearchRequests = maxInt(dst.ServerToolUse.WebSearchRequests, src.ServerToolUse.WebSearchRequests)
			dst.ServerToolUse.WebFetchRequests = maxInt(dst.ServerToolUse.WebFetchRequests, src.ServerToolUse.WebFetchRequests)
		}
	}
	if dst.ServiceTier == "" && src.ServiceTier != "" {
		dst.ServiceTier = src.ServiceTier
	}
	if dst.InferenceGeo == "" && src.InferenceGeo != "" {
		dst.InferenceGeo = src.InferenceGeo
	}
}

func maxInt(a, b int) int {
	if a >= b {
		return a
	}
	return b
}

func conversationUsageDedupKey(record conversationRecord) string {
	if record.requestID != "" {
		return "req:" + record.requestID
	}
	if record.messageID != "" {
		return "msg:" + record.messageID
	}
	if record.usage == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%d|%d|%d|%d|%d",
		record.sessionID,
		record.timestamp.UTC().Format(time.RFC3339Nano),
		record.usage.InputTokens,
		record.usage.OutputTokens,
		record.usage.CacheReadInputTokens,
		record.usage.CacheCreationInputTokens,
		record.usage.ReasoningTokens,
	)
}

func conversationToolDedupKey(record conversationRecord, idx int, item jsonlContent) string {
	base := record.requestID
	if base == "" {
		base = record.messageID
	}
	if base == "" {
		base = record.sessionID + "|" + record.timestamp.UTC().Format(time.RFC3339Nano)
	}
	if item.ID != "" {
		return base + "|tool|" + item.ID
	}
	name := strings.ToLower(strings.TrimSpace(item.Name))
	if name == "" {
		name = "unknown"
	}
	return fmt.Sprintf("%s|tool|%s|%d", base, name, idx)
}

func conversationTotalTokens(usage *jsonlUsage) int64 {
	if usage == nil {
		return 0
	}
	return int64(
		usage.InputTokens +
			usage.OutputTokens +
			usage.CacheReadInputTokens +
			usage.CacheCreationInputTokens +
			usage.ReasoningTokens,
	)
}
