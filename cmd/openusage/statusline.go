package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/claude_code"
	"github.com/janekbaraniewski/openusage/internal/report"
)

// statuslineInput is the JSON Claude Code pipes to a statusLine command on
// stdin. Only the fields we use are declared; unknown fields are ignored.
type statuslineInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	Model          struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
		ProjectDir string `json:"project_dir"`
	} `json:"workspace"`
	Cost struct {
		TotalCostUSD float64 `json:"total_cost_usd"`
	} `json:"cost"`
}

type statuslineOptions struct {
	offline       bool
	mode          string
	color         bool
	contextMedium float64
	contextHigh   float64
}

// settingsSnippet is shown in --help so users can wire the statusline into
// Claude Code.
const settingsSnippet = `To wire it in automatically (creates a .bak backup, preserves other keys):

  openusage statusline --install

Or add this to ~/.claude/settings.json by hand:

  {
    "statusLine": {
      "type": "command",
      "command": "openusage statusline",
      "padding": 0
    }
  }`

func newStatuslineCommand() *cobra.Command {
	opts := statuslineOptions{
		offline:       true,
		mode:          string(claude_code.CostModeCalculate),
		color:         true,
		contextMedium: 50,
		contextHigh:   80,
	}
	var install, uninstall bool

	cmd := &cobra.Command{
		Use:   "statusline",
		Short: "Emit a one-line Claude Code status bar (session/today/block cost, burn rate, context)",
		Long: `Render a single status line for the Claude Code status bar.

Claude Code pipes the active session JSON to this command on stdin; the output
is one line summarizing the current model, session/today/active-block cost, the
burn rate, and context-window usage. Costs are API-equivalent estimates derived
from the local conversation logs, not subscription charges.

It runs offline by default (embedded pricing) so it responds instantly; pass
--offline=false to fetch live pricing.

` + settingsSnippet,
		Example: strings.Join([]string{
			`  echo '{"session_id":"abc","model":{"display_name":"Opus 4.8"}}' | openusage statusline`,
			"  openusage statusline --offline=false",
		}, "\n"),
		RunE: func(_ *cobra.Command, _ []string) error {
			switch {
			case install:
				return installStatusline(os.Stdout)
			case uninstall:
				return uninstallStatusline(os.Stdout)
			default:
				return runStatusline(opts, os.Stdin, os.Stdout)
			}
		},
	}

	fl := cmd.Flags()
	fl.BoolVar(&opts.offline, "offline", opts.offline, "use embedded pricing and skip network lookups")
	fl.StringVar(&opts.mode, "mode", opts.mode, "cost mode: calculate, display, or auto")
	fl.BoolVar(&opts.color, "color", opts.color, "colorize the output with ANSI escapes")
	fl.Float64Var(&opts.contextMedium, "context-medium", opts.contextMedium, "context %% threshold for the yellow warning color")
	fl.Float64Var(&opts.contextHigh, "context-high", opts.contextHigh, "context %% threshold for the red warning color")
	fl.BoolVar(&install, "install", false, "wire this statusline into ~/.claude/settings.json (creates a .bak backup)")
	fl.BoolVar(&uninstall, "uninstall", false, "remove the openusage statusline from ~/.claude/settings.json")
	cmd.MarkFlagsMutuallyExclusive("install", "uninstall")

	return cmd
}

// claudeSettingsPath resolves the Claude Code settings.json, honoring the
// CLAUDE_SETTINGS_FILE override used elsewhere in the codebase.
func claudeSettingsPath() string {
	if f := strings.TrimSpace(os.Getenv("CLAUDE_SETTINGS_FILE")); f != "" {
		return f
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// statuslineCommandString returns the command Claude Code should invoke: the
// resolved openusage binary plus the statusline subcommand.
func statuslineCommandString() string {
	bin, err := os.Executable()
	if err != nil || strings.TrimSpace(bin) == "" {
		bin = "openusage"
	}
	return bin + " statusline"
}

// installStatusline merges the statusLine block into settings.json, preserving
// every other key and backing up the original file first.
func installStatusline(out io.Writer) error {
	path := claudeSettingsPath()
	cfg, err := readJSONObject(path)
	if err != nil {
		return err
	}
	cfg["statusLine"] = map[string]any{
		"type":    "command",
		"command": statuslineCommandString(),
		"padding": 0,
	}
	if err := writeJSONObjectWithBackup(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "installed statusline into %s\n", path)
	fmt.Fprintf(out, "  command: %s\n", statuslineCommandString())
	return nil
}

// uninstallStatusline removes our statusLine block when it points at openusage.
func uninstallStatusline(out io.Writer) error {
	path := claudeSettingsPath()
	cfg, err := readJSONObject(path)
	if err != nil {
		return err
	}
	if sl, ok := cfg["statusLine"].(map[string]any); ok {
		if cmd, _ := sl["command"].(string); strings.Contains(cmd, "statusline") && strings.Contains(cmd, "openusage") {
			delete(cfg, "statusLine")
			if err := writeJSONObjectWithBackup(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(out, "removed statusline from %s\n", path)
			return nil
		}
		fmt.Fprintf(out, "statusLine in %s is not managed by openusage; left unchanged\n", path)
		return nil
	}
	fmt.Fprintf(out, "no statusLine configured in %s\n", path)
	return nil
}

func readJSONObject(path string) (map[string]any, error) {
	cfg := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

func writeJSONObjectWithBackup(path string, cfg map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		if err := os.WriteFile(path+".bak", existing, 0o600); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
	}
	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize settings: %w", err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func runStatusline(opts statuslineOptions, stdin io.Reader, stdout io.Writer) error {
	in := readStatuslineInput(stdin)

	events, err := claudeCodeConversationEvents(claude_code.ParseCostMode(opts.mode), opts.offline)
	if err != nil {
		// Without logs we can still echo the model and Claude Code's own cost.
		fmt.Fprintln(stdout, renderStatusline(in, nil, time.Now(), opts))
		return nil
	}
	fmt.Fprintln(stdout, renderStatusline(in, events, time.Now(), opts))
	return nil
}

// readStatuslineInput reads and decodes the stdin payload. A terminal (no pipe)
// or malformed JSON yields a zero-value input so the command still renders.
func readStatuslineInput(stdin io.Reader) statuslineInput {
	var in statuslineInput
	if f, ok := stdin.(*os.File); ok {
		if info, err := f.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			return in // interactive terminal: nothing piped in
		}
	}
	data, err := io.ReadAll(stdin)
	if err != nil || len(data) == 0 {
		return in
	}
	_ = json.Unmarshal(data, &in)
	return in
}

// renderStatusline builds the status line. It is pure (no I/O) so it can be
// unit-tested with synthetic events.
func renderStatusline(in statuslineInput, events []report.Event, now time.Time, opts statuslineOptions) string {
	model := strings.TrimSpace(in.Model.DisplayName)
	if model == "" {
		model = shortModelID(in.Model.ID)
	}

	var (
		sessionCost  float64
		todayCost    float64
		contextTok   int
		haveSession  bool
		midnight     = core.LocalMidnight()
		lastSessTime time.Time
	)
	for _, e := range events {
		if !e.Time.Before(midnight) {
			todayCost += e.Cost
		}
		if in.SessionID != "" && e.Session == in.SessionID {
			sessionCost += e.Cost
			haveSession = true
			if !e.Time.Before(lastSessTime) {
				lastSessTime = e.Time
				contextTok = e.Input + e.CacheRead + e.CacheCreate
				if model == "" {
					model = shortModelID(e.Model)
				}
			}
		}
	}
	// Fall back to Claude Code's own session cost when we have no matching logs.
	if !haveSession && in.Cost.TotalCostUSD > 0 {
		sessionCost = in.Cost.TotalCostUSD
	}
	if model == "" {
		model = "claude"
	}

	// Active billing block.
	var (
		blockCost float64
		blockLeft time.Duration
		burn      float64
		haveBlock bool
	)
	if len(events) > 0 {
		rep := report.Build(events, report.Options{Kind: report.KindBlocks, Now: now})
		if active, ok := rep.ActiveBlock(); ok {
			blockCost = active.Cost
			blockLeft = active.TimeRemaining
			burn = active.BurnRateUSDPerHour
			haveBlock = true
		}
	}

	ctxWindow := contextWindowFor(in.Model.ID)
	// If the observed context already exceeds the guessed window, the session
	// is on the 1M-token tier; correct the denominator so the percentage stays
	// meaningful offline (where we can't consult model metadata).
	if contextTok > ctxWindow {
		ctxWindow = 1_000_000
	}
	ctxPct := 0.0
	if ctxWindow > 0 && contextTok > 0 {
		ctxPct = float64(contextTok) / float64(ctxWindow) * 100
		if ctxPct > 100 {
			ctxPct = 100
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "🤖 %s", model)
	fmt.Fprintf(&b, " | 💰 $%.2f sess / $%.2f today", sessionCost, todayCost)
	if haveBlock {
		fmt.Fprintf(&b, " / $%.2f block (%s left)", blockCost, fmtStatusDuration(blockLeft))
		if burn > 0 {
			fmt.Fprintf(&b, " | 🔥 %s/hr", colorize(fmt.Sprintf("$%.2f", burn), ansiOrange, opts.color))
		}
	}
	if contextTok > 0 {
		ctxStr := fmt.Sprintf("🧠 %s", fmtTokensShort(contextTok))
		if ctxPct > 0 {
			ctxStr += fmt.Sprintf(" (%.0f%%)", ctxPct)
		}
		fmt.Fprintf(&b, " | %s", colorize(ctxStr, contextColor(ctxPct, opts), opts.color))
	}
	return b.String()
}

// --- helpers ---

const (
	ansiReset  = "\033[0m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiOrange = "\033[38;5;208m" // approx brand orange
)

func colorize(s, code string, enabled bool) string {
	if !enabled || code == "" {
		return s
	}
	return code + s + ansiReset
}

func contextColor(pct float64, opts statuslineOptions) string {
	switch {
	case pct >= opts.contextHigh:
		return ansiRed
	case pct >= opts.contextMedium:
		return ansiYellow
	default:
		return ansiGreen
	}
}

func contextWindowFor(modelID string) int {
	id := strings.ToLower(modelID)
	if strings.Contains(id, "1m") {
		return 1_000_000
	}
	// Claude models are 200k by default.
	return 200_000
}

func shortModelID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if i := strings.LastIndex(id, "/"); i >= 0 && i < len(id)-1 {
		id = id[i+1:]
	}
	return id
}

func fmtStatusDuration(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func fmtTokensShort(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
