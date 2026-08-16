#!/usr/bin/env bash
# OpenUsage SketchyBar detail popup. Data and labels come from the CLI.

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
OPENUSAGE_BIN="${OPENUSAGE_BIN:-openusage}"
CACHE_DIR="${OPENUSAGE_SKETCHYBAR_CACHE_DIR:-$HOME/.cache/openusage/sketchybar}"
DETAIL_CACHE="$CACHE_DIR/detail.json"
STALE_AFTER="${OPENUSAGE_SKETCHYBAR_STALE_AFTER:-600}"
PARENT="${NAME:-ai}"

file_mtime() {
  stat -f %m "$1" 2>/dev/null || stat -c %Y "$1" 2>/dev/null || printf '0\n'
}

valid_detail() {
  jq -e 'type == "object" and (.rows | type == "array")' "$1" >/dev/null 2>&1
}

mkdir -p "$CACHE_DIR" 2>/dev/null
payload=""
if payload=$("$OPENUSAGE_BIN" active detail --json 2>/dev/null) &&
   printf '%s\n' "$payload" | jq -e 'type == "object" and (.rows | type == "array")' >/dev/null 2>&1; then
  tmp=$(mktemp "$CACHE_DIR/detail.XXXXXX" 2>/dev/null) || tmp=""
  if [ -n "$tmp" ]; then
    printf '%s\n' "$payload" >"$tmp" && mv -f "$tmp" "$DETAIL_CACHE"
  fi
fi
if [ -z "$payload" ] && [ -f "$DETAIL_CACHE" ] && valid_detail "$DETAIL_CACHE"; then
  payload=$(<"$DETAIL_CACHE")
fi

sketchybar --remove '/openusage.usage.row\..*/' >/dev/null 2>&1

if [ -z "$payload" ] || ! printf '%s\n' "$payload" | jq -e 'type == "object"' >/dev/null 2>&1; then
  lines=$'provider\tAI unavailable'
else
  lines=$(printf '%s\n' "$payload" | jq -r '
    [
      ["provider", (.selection.display // "AI") + " " + (.selection.label // "quota unavailable")],
      (if (.message // "") != "" then ["message", .message] else empty end),
      (.rows[] | select((.display // "") != "") | [.name, .display])
    ][] | @tsv
  ')
fi

if [ -f "$DETAIL_CACHE" ]; then
  now=$(date +%s)
  age=$((now - $(file_mtime "$DETAIL_CACHE")))
  if [ "$age" -gt "$STALE_AFTER" ]; then
    lines="$lines"$'\nstatus\tstale data'
  fi
fi

index=0
while IFS=$'\t' read -r name value; do
  [ -z "$name" ] && continue
  item="openusage.usage.row.$index"
  label="$name"
  [ "$name" = "provider" ] && label="provider"
  [ "$name" = "message" ] && label="message"
  [ "$name" = "status" ] && label="status"
  sketchybar --add item "$item" "popup.$PARENT" \
    --set "$item" label="$label  $value" \
      label.font="Hack Nerd Font:Regular:12.0" \
      label.color="${OPENUSAGE_SKETCHYBAR_TEXT_COLOR:-0xffcad3f5}" \
      icon.drawing=off background.padding_left=6 background.padding_right=6
  index=$((index + 1))
done <<< "$lines"

sketchybar --set "$PARENT" popup.drawing=on
