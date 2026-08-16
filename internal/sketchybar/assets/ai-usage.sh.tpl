#!/usr/bin/env bash
# OpenUsage SketchyBar bar item. Keep this script boring: the CLI owns
# provider selection and narration; this file only paints the result.

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
OPENUSAGE_BIN="${OPENUSAGE_BIN:-openusage}"
CACHE_DIR="${OPENUSAGE_SKETCHYBAR_CACHE_DIR:-$HOME/.cache/openusage/sketchybar}"
ACTIVE_CACHE="$CACHE_DIR/active.json"
STALE_AFTER="${OPENUSAGE_SKETCHYBAR_STALE_AFTER:-600}"

file_mtime() {
  stat -f %m "$1" 2>/dev/null || stat -c %Y "$1" 2>/dev/null || printf '0\n'
}

valid_active() {
  jq -e 'type == "object" and (.status | type == "string")' "$1" >/dev/null 2>&1
}

paint() {
  local text="$1"
  local color="$2"
  sketchybar --set "${NAME:-ai}" drawing=on \
    icon="${OPENUSAGE_SKETCHYBAR_ICON:-󰚩}" icon.color="$color" \
    label="$text" label.color="$color"
}

case "${SENDER:-}" in
  mouse.entered)
    exec "$SCRIPT_DIR/usage-popup.sh"
    ;;
  mouse.exited|mouse.exited.global)
    sketchybar --set "${NAME:-ai}" popup.drawing=off
    sketchybar --set ai_switcher popup.drawing=off >/dev/null 2>&1
    exit 0
    ;;
esac

mkdir -p "$CACHE_DIR" 2>/dev/null
payload=""
if payload=$("$OPENUSAGE_BIN" active --json 2>/dev/null) &&
   printf '%s\n' "$payload" | jq -e 'type == "object" and (.status | type == "string")' >/dev/null 2>&1; then
  tmp=$(mktemp "$CACHE_DIR/active.XXXXXX" 2>/dev/null) || tmp=""
  if [ -n "$tmp" ]; then
    printf '%s\n' "$payload" >"$tmp" && mv -f "$tmp" "$ACTIVE_CACHE"
  fi
fi

if [ -z "$payload" ] && [ -f "$ACTIVE_CACHE" ] && valid_active "$ACTIVE_CACHE"; then
  payload=$(<"$ACTIVE_CACHE")
fi

if [ -z "$payload" ] || ! printf '%s\n' "$payload" | jq -e 'type == "object"' >/dev/null 2>&1; then
  paint "AI unavailable" "${OPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR:-0xffcad3f5}"
  exit 0
fi

status=$(printf '%s\n' "$payload" | jq -r '.status // "unavailable"')
if [ "$status" != "ok" ]; then
  case "$status" in
    no_data) paint "AI no data" "${OPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR:-0xffcad3f5}" ;;
    *)       paint "AI $status" "${OPENUSAGE_SKETCHYBAR_WARN_COLOR:-0xffeed49f}" ;;
  esac
  exit 0
fi

display=$(printf '%s\n' "$payload" | jq -r '.display // "AI"')
label=$(printf '%s\n' "$payload" | jq -r '.label // "quota unavailable"')
severity=$(printf '%s\n' "$payload" | jq -r '.severity // "unknown"')
color="${OPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR:-0xffcad3f5}"
case "$severity" in
  good) color="${OPENUSAGE_SKETCHYBAR_GOOD_COLOR:-0xffa6da95}" ;;
  warn) color="${OPENUSAGE_SKETCHYBAR_WARN_COLOR:-0xffeed49f}" ;;
  bad)  color="${OPENUSAGE_SKETCHYBAR_BAD_COLOR:-0xffed8796}" ;;
esac

if [ -f "$ACTIVE_CACHE" ]; then
  now=$(date +%s)
  age=$((now - $(file_mtime "$ACTIVE_CACHE")))
  if [ "$age" -gt "$STALE_AFTER" ]; then
    label="$label · stale"
    color="${OPENUSAGE_SKETCHYBAR_WARN_COLOR:-0xffeed49f}"
  fi
fi

paint "$display $label" "$color"
