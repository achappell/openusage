package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/janekbaraniewski/openusage/internal/active"
	"github.com/janekbaraniewski/openusage/internal/daemon"
)

// activeTimeout caps how long the CLI waits on the daemon. A status-bar item
// polls on a short interval and must degrade rather than hang.
const activeTimeout = 2 * time.Second

type activeOptions struct {
	socketPath string
	asJSON     bool
	explain    bool
	out        io.Writer
}

func newActiveCommand() *cobra.Command {
	opts := activeOptions{out: os.Stdout}

	cmd := &cobra.Command{
		Use:   "active",
		Short: "Show which AI provider is currently active",
		Long: `Report the provider you are currently working with, along with its quota
position and a ready-to-render status label.

Selection prefers firsthand telemetry events from the running daemon. When the
daemon is unreachable it falls back to local-file recency detection, which is
less precise and cannot see API-key-only providers; responses then report
source "local".`,
		Example: strings.Join([]string{
			"  openusage active --json",
			"  openusage active pin codex:default",
			"  openusage active unpin",
		}, "\n"),
		RunE: func(_ *cobra.Command, _ []string) error { return runActive(opts) },
	}
	cmd.PersistentFlags().StringVar(&opts.socketPath, "socket-path", "", "path to the telemetry daemon unix socket")
	cmd.Flags().BoolVar(&opts.asJSON, "json", false, "emit JSON instead of a plain label")
	cmd.Flags().BoolVar(&opts.explain, "explain", false, "print why this provider was selected, then exit")

	pin := &cobra.Command{
		Use:   "pin <provider:account>",
		Short: "Pin a provider until activity appears elsewhere",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return setActivePin(opts.socketPath, args[0])
		},
	}
	unpin := &cobra.Command{
		Use:   "unpin",
		Short: "Clear the active-provider pin",
		RunE: func(_ *cobra.Command, _ []string) error {
			return setActivePin(opts.socketPath, "")
		},
	}
	cmd.AddCommand(pin, unpin)
	return cmd
}

func runActive(opts activeOptions) error {
	if opts.out == nil {
		opts.out = os.Stdout
	}
	if opts.explain {
		return runActiveExplain(opts)
	}
	sel := resolveActive(opts.socketPath)
	if !opts.asJSON {
		if sel.Status != "ok" {
			_, err := fmt.Fprintf(opts.out, "AI %s\n", sel.Status)
			return err
		}
		_, err := fmt.Fprintf(opts.out, "%s %s\n", sel.Display, sel.Label)
		return err
	}
	enc := json.NewEncoder(opts.out)
	enc.SetIndent("", "  ")
	return enc.Encode(sel)
}

func runActiveExplain(opts activeOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), activeTimeout)
	defer cancel()
	if explanation, err := daemon.NewClient(resolveActiveSocketPath(opts.socketPath)).ActiveExplain(ctx); err == nil {
		_, writeErr := io.WriteString(opts.out, explanation)
		if writeErr != nil {
			return writeErr
		}
		if !strings.HasSuffix(explanation, "\n") {
			_, writeErr = io.WriteString(opts.out, "\n")
		}
		return writeErr
	}

	// The local detector is deliberately not dressed up as an event-based
	// explanation. It has different evidence, so say exactly which path won.
	res := active.Detect(active.DetectOptions{})
	if res.Primary == "" {
		_, err := fmt.Fprintln(opts.out, "daemon unavailable; local mtime detection found no candidates")
		return err
	}
	_, err := fmt.Fprintf(opts.out,
		"daemon unavailable; using local mtime detection\nwinner: %s (source: %s)\n",
		res.Primary, res.Source)
	return err
}

// resolveActive asks the daemon, falling back to local mtime detection.
func resolveActive(socketPath string) active.Selection {
	socketPath = resolveActiveSocketPath(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), activeTimeout)
	defer cancel()

	if sel, err := daemon.NewClient(socketPath).Active(ctx); err == nil {
		return sel
	}

	// Degraded: reuse the existing mtime detector rather than inventing a
	// second inference path.
	res := active.Detect(active.DetectOptions{})
	if res.Primary == "" {
		return active.Selection{Status: "no_data", Severity: active.SeverityUnknown, Source: "local"}
	}
	return active.Selection{
		Selected: res.Primary + ":default",
		Display:  active.DisplayName(res.Primary),
		Severity: active.SeverityUnknown,
		Label:    "quota unavailable",
		Source:   "local",
		Status:   "ok",
	}
}

func resolveActiveSocketPath(socketPath string) string {
	if strings.TrimSpace(socketPath) != "" {
		return strings.TrimSpace(socketPath)
	}
	return daemon.ResolveSocketPath()
}

func setActivePin(socketPath, key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), activeTimeout)
	defer cancel()
	if err := daemon.NewClient(resolveActiveSocketPath(socketPath)).SetPin(ctx, key); err != nil {
		return fmt.Errorf("active: setting pin (is the daemon running?): %w", err)
	}
	return nil
}
