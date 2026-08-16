---
title: SketchyBar integration — active AI provider usage
description: Show the active AI provider and quota position in SketchyBar with OpenUsage, including a detail popup and provider switcher.
keywords: [sketchybar ai usage, sketchybar quota, openusage sketchybar, macOS AI status bar]
sidebar_label: SketchyBar integration
---

# SketchyBar integration

OpenUsage can add three small pieces to a [SketchyBar](https://github.com/FelixKratz/SketchyBar) setup:

- an `ai` item showing the active provider and its quota label;
- a detail popup with the selected provider's metric rows;
- an `ai_switcher` popup that pins a provider account or returns to automatic selection.

The data path is deliberately local. The scripts call `openusage active` over
the telemetry daemon's Unix socket, and the CLI gives up after two seconds so
SketchyBar never waits for a network request. If the daemon is down, the bar
uses the CLI's degraded local detector and paints the last successful value as
stale after ten minutes.

## Requirements

- macOS with SketchyBar installed and running;
- `openusage` on `PATH`, or a binary path passed with `--binary`;
- `jq` on `PATH` for the generated shell scripts;
- the OpenUsage telemetry daemon if you want event-backed active-provider selection.

Start the daemon if it is not already installed:

```bash
openusage telemetry daemon install
openusage telemetry daemon status
```

## Install

The installer writes generated scripts to
`~/.local/share/openusage/sketchybar/` and inserts one sentinel block into
`~/.config/sketchybar/sketchybarrc`:

```bash
openusage sketchybar install --write
sketchybar --reload
```

Re-running the command replaces only the OpenUsage block and creates a
`sketchybarrc.bak` backup when the config already exists. It never writes into
`~/.config/sketchybar/plugins/`; that directory is often a set of symlinks into
a dotfiles repository, and OpenUsage has no business rummaging around in it.

Use an explicit path when your config lives elsewhere:

```bash
openusage sketchybar install --write \
  --config ~/dotfiles/sketchybar/sketchybarrc \
  --binary ~/bin/openusage
```

Without `--write`, the command prints the complete managed block so you can
review it or paste it into a config by hand:

```bash
openusage sketchybar
```

## Full managed snippet

Run the installer once so the three scripts exist, then this is the complete
block the generated config contains. The sentinel comments are intentional;
leave them in place so future installs can replace the block safely.

```bash
# >>> openusage sketchybar >>> (managed; do not edit between sentinels)
# Generated scripts live outside ~/.config/sketchybar/plugins.
OPENUSAGE_SKETCHYBAR_DIR="$HOME/.local/share/openusage/sketchybar"
OPENUSAGE_BIN="openusage"
OPENUSAGE_SKETCHYBAR_CACHE_DIR="${OPENUSAGE_SKETCHYBAR_CACHE_DIR:-$HOME/.cache/openusage/sketchybar}"
OPENUSAGE_SKETCHYBAR_ICON="󰚩"
OPENUSAGE_SKETCHYBAR_GOOD_COLOR="0xffa6da95"
OPENUSAGE_SKETCHYBAR_WARN_COLOR="0xffeed49f"
OPENUSAGE_SKETCHYBAR_BAD_COLOR="0xffed8796"
OPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR="0xffcad3f5"
OPENUSAGE_SKETCHYBAR_TEXT_COLOR="0xffcad3f5"
export OPENUSAGE_SKETCHYBAR_DIR OPENUSAGE_BIN OPENUSAGE_SKETCHYBAR_CACHE_DIR
export OPENUSAGE_SKETCHYBAR_ICON OPENUSAGE_SKETCHYBAR_GOOD_COLOR OPENUSAGE_SKETCHYBAR_WARN_COLOR OPENUSAGE_SKETCHYBAR_BAD_COLOR OPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR OPENUSAGE_SKETCHYBAR_TEXT_COLOR
sketchybar --add item ai right >/dev/null 2>&1 || true
sketchybar --subscribe ai mouse.entered mouse.exited mouse.exited.global
sketchybar --set ai update_freq=60 padding_left=10 popup.background.color="0xff1e2030" popup.background.border_color="0xff494d64" popup.background.border_width=2 popup.background.corner_radius=6 popup.align=right popup.y_offset=2 script="$OPENUSAGE_SKETCHYBAR_DIR/ai-usage.sh"
sketchybar --add item ai_switcher right >/dev/null 2>&1 || true
sketchybar --subscribe ai_switcher mouse.entered
sketchybar --set ai_switcher padding_left=2 padding_right=2 icon="⇄" icon.font="Hack Nerd Font:Regular:13.0" icon.color="0xffcad3f5" label.drawing=off popup.background.color="0xff1e2030" popup.background.border_color="0xff494d64" popup.background.border_width=2 popup.background.corner_radius=6 popup.horizontal=on popup.align=right popup.y_offset=2 script="$OPENUSAGE_SKETCHYBAR_DIR/provider-select.sh"
sketchybar --update
# <<< openusage sketchybar <<<
```

The generated `ai-usage.sh` script opens the detail popup on hover. The
switcher script uses the stable `provider:account` key from
`openusage active list --json`, so account labels and display text are never
parsed back into selection state.

## Presets

The default preset is `catppuccin-macchiato`, matching the reference
configuration's right-side items and palette. List or inspect presets with:

```bash
openusage sketchybar presets
openusage sketchybar presets --show catppuccin-macchiato
openusage sketchybar install --write --preset catppuccin-macchiato
```

Presets control the item names, position, refresh interval, icon, popup shape,
and severity colors. The quota thresholds and selection rules remain in
OpenUsage's shared active-provider core.

## Inspect and control the data

These commands are useful when the bar looks wrong:

```bash
openusage active --json
openusage active detail --json
openusage active list --json
openusage active --explain
openusage active pin codex:default
openusage active unpin
```

The `active` command prefers a live pin, then the newest telemetry event. With
no telemetry events it uses configured provider priority. A pin releases when
another provider records a newer user-activity event.

## Doctor and uninstall

```bash
openusage sketchybar doctor
openusage sketchybar uninstall
sketchybar --reload
```

Uninstall removes the sentinel block and leaves the neutral generated scripts
in place. That is deliberate: it makes rollback and a later reinstall safe,
without deleting files a user may have inspected or customized.

## Troubleshooting

### The item says “AI unavailable”

Check the executable and daemon first:

```bash
openusage sketchybar doctor
openusage telemetry daemon status
openusage active --json
```

If `active --json` reports `source: "local"`, the daemon was unreachable and
OpenUsage is using its degraded local detector. That path cannot identify
API-key-only providers; start the daemon and install the relevant hook/plugin
integration for event-backed selection.

### The popup is empty

Install `jq`, then reload SketchyBar. The popup and switcher scripts are plain
Bash plus `jq`; they do not require Python or a package manager.

```bash
brew install jq
sketchybar --reload
```

### The config is symlinked into dotfiles

That is supported. The installer follows the config symlink and edits its
target while preserving the symlink itself. It still writes generated scripts
only under OpenUsage's neutral data directory.
