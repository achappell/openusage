package claude_code

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// detectAgentType resolves the agent label for a Claude Code subagent session
// file using a three-tier strategy:
//
//  1. Probe a `<basename>.meta.json` sidecar in the same directory; if present
//     and it carries a non-empty `agentType` field, return that value.
//  2. Cross-reference the parent JSONL transcript (one directory up from
//     `subagents/`): scan for `tool_use` blocks where `input.subagent_type`
//     correlates with this file's `agentId`. Return the matching
//     `subagent_type` when one is found.
//  3. Fall back to a generic `"agent"` label.
//
// Main-session files (those NOT located under a `subagents/` directory) are
// always labelled `"main"`.
func detectAgentType(filePath string) string {
	if filePath == "" {
		return "main"
	}
	if !pathContainsSubagentsDir(filePath) {
		return "main"
	}

	// Tier 1: .meta.json sidecar.
	if label := readAgentTypeFromMeta(filePath); label != "" {
		return label
	}

	// Tier 2: parent JSONL tool_use cross-join.
	agentID := extractAgentIDFromFilename(filePath)
	if agentID != "" {
		parentJSONL := locateParentSessionJSONL(filePath)
		if parentJSONL != "" {
			if label := lookupSubagentTypeInParent(parentJSONL, agentID); label != "" {
				return label
			}
		}
	}

	// Tier 3: generic fallback. We keep it terse so analytics renders it
	// nicely as a column in an Agents view.
	return "agent"
}

func pathContainsSubagentsDir(p string) bool {
	sep := string(filepath.Separator)
	return strings.Contains(p, sep+"subagents"+sep)
}

// readAgentTypeFromMeta reads `<filePath>.meta.json` (i.e. the JSONL filename
// with `.meta.json` appended in place of `.jsonl`) and returns the agentType
// field if present.
func readAgentTypeFromMeta(filePath string) string {
	base := strings.TrimSuffix(filePath, filepath.Ext(filePath))
	metaPath := base + ".meta.json"
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return ""
	}
	var meta struct {
		AgentType string `json:"agentType"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	return strings.TrimSpace(meta.AgentType)
}

// extractAgentIDFromFilename pulls an agent identifier out of the basename.
// Claude Code subagent files are typically named `agent-<id>.jsonl`; we
// preserve whatever follows `agent-` up to the extension so the caller can
// correlate against parent transcripts.
func extractAgentIDFromFilename(filePath string) string {
	base := filepath.Base(filePath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if strings.HasPrefix(base, "agent-") {
		return strings.TrimPrefix(base, "agent-")
	}
	return base
}

// locateParentSessionJSONL returns the path to the parent session JSONL for a
// subagent file. Subagent files live at
// `<projects>/<key>/<session>/subagents/agent-X.jsonl`; the parent transcript
// is `<projects>/<key>/<session>.jsonl` or a JSONL sibling of the `subagents`
// directory. We try both layouts.
func locateParentSessionJSONL(filePath string) string {
	subagentsDir := filepath.Dir(filePath)
	if filepath.Base(subagentsDir) != "subagents" {
		return ""
	}
	parentDir := filepath.Dir(subagentsDir)

	// Layout A: <session>/subagents/agent-X.jsonl with the parent being
	// `<session>.jsonl` one level up (alongside the `<session>` directory).
	sessionName := filepath.Base(parentDir)
	candidate := filepath.Join(filepath.Dir(parentDir), sessionName+".jsonl")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}

	// Layout B: any sibling `.jsonl` inside the parent directory itself.
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".jsonl") {
			return filepath.Join(parentDir, name)
		}
	}
	return ""
}

// lookupSubagentTypeInParent scans a parent session JSONL looking for a
// `tool_use` content block whose `input.subagent_type` corresponds to the
// given `agentID`. The correlation is loose by design: some transcripts
// embed the agent id in the tool input, others in the subsequent
// `tool_result`. We accept any tool_use whose serialised input or sibling
// tool_result mentions the agent id substring.
func lookupSubagentTypeInParent(parentJSONL, agentID string) string {
	f, err := os.Open(parentJSONL)
	if err != nil {
		return ""
	}
	defer f.Close()

	type contentBlock struct {
		Type    string          `json:"type"`
		ID      string          `json:"id,omitempty"`
		Name    string          `json:"name,omitempty"`
		Input   json.RawMessage `json:"input,omitempty"`
		Content json.RawMessage `json:"content,omitempty"`
	}
	type message struct {
		Content []contentBlock `json:"content,omitempty"`
	}
	type entry struct {
		Message *message `json:"message,omitempty"`
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		if e.Message == nil {
			continue
		}
		for _, block := range e.Message.Content {
			if block.Type != "tool_use" {
				continue
			}
			var input map[string]any
			if err := json.Unmarshal(block.Input, &input); err != nil {
				continue
			}
			subagentType, _ := input["subagent_type"].(string)
			if subagentType == "" {
				continue
			}
			// Correlate by agent id substring across the serialised input
			// (some clients store the id under varying keys).
			if jsonContainsAgentID(block.Input, agentID) {
				return strings.TrimSpace(subagentType)
			}
		}
	}
	return ""
}

// jsonContainsAgentID returns true when the agent id appears verbatim inside
// the JSON payload. The check is intentionally substring-based so it tolerates
// the various places (e.g. `agent_id`, `agentId`, `description`) where the
// reference id can land.
func jsonContainsAgentID(raw json.RawMessage, agentID string) bool {
	if len(raw) == 0 || agentID == "" {
		return false
	}
	return strings.Contains(string(raw), agentID)
}
