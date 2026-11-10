---
title: Codex CLI
description: Track OpenAI Codex CLI sessions, rate limits, and credit balance in OpenUsage.
sidebar_label: Codex
keywords: [codex cli usage tracker, codex cli quota tracking, codex cli cost tracking, codex cli token usage, track codex cli spend locally]
---

# Codex CLI

Local-file provider for the OpenAI Codex CLI. Reads session logs, auth state, and config to show today's activity, plan info, and rate-limit windows.

## At a glance

- **Provider ID** — `codex`
- **Detection** — `~/.codex` directory on disk
- **Auth** — token stored in `~/.codex/auth.json` by the Codex CLI; no env var needed
- **Type** — coding agent
- **Tracks**:
  - Latest session: tokens, model, client
  - Daily session counts
  - Model and client breakdowns
  - Rate-limit windows (primary and secondary)
  - Individual credit usage versus the current monthly limit
  - Credit burn rate, expected usage at reset, and projected reserve
  - Plan and version
  - Patch stats

## Setup

### Auto-detection

OpenUsage registers the provider once Codex has written data under `~/.codex/`. Run the Codex CLI at least once to create it.

Detection needs `~/.codex/sessions/` or `~/.codex/auth.json`; the `codex` binary is optional. The telemetry daemon runs under launchd or systemd with a minimal `PATH`, so OpenUsage also looks for `codex` in common per-user install directories (`~/.local/bin`, `~/.npm-global/bin`, `~/.nvm/versions/node/*/bin`, `~/.volta/bin`, `~/.bun/bin`, pnpm and Linuxbrew prefixes). Without the binary, quota comes from the live usage endpoint instead of `codex app-server`.

### Manual configuration

Normally no manual account is needed. To set a lower personal credit budget, open **Settings → Providers**, select `codex-cli`, and press `l`. You can also configure the same advisory cap in JSON:

```json
{
  "accounts": [
    {
      "id": "codex-cli",
      "provider": "codex",
      "auth": "local",
      "binary": "codex",
      "credit_limit_override": 4000
    }
  ]
}
```

The personal cap is advisory: OpenUsage uses it for the credit percentage, burn forecast, runout estimate, and `LIMITED` status, but Codex continues to use its provider-reported quota. OpenUsage keeps that reported quota visible in the Codex detail view. Omit the field, clear it in Settings, or remove the manual account to restore provider-quota behavior.

## Data sources & how each metric is computed

Codex has four data paths:

1. **Codex CLI app-server** — `codex app-server --stdio` reads `account/rateLimits/read` before the session scan, supplying quota windows, plan, credits, and individual monthly credit limits.
2. **Live ChatGPT usage endpoint** — an authenticated GET, attempted concurrently with the app-server when `auth.json` contains an access token. Live quota takes precedence for a pool when both sources report it; the app-server can supply other pools and missing credit details.
3. **Local files** — JSONL session transcripts (plain `.jsonl` or zstd-compressed `.jsonl.zst`) and auth/config metadata under `~/.codex/`. The latest usable session supplies token activity and fallback quota windows when neither live source supplies a usable window.
4. **Daily account-credit endpoint** — an authenticated GET for the current billing period, broken down by product. It provides historical `codex` credit totals; OpenUsage fills missing days with zeroes and replaces today's value with the live cumulative total minus historical days.

A live quota window replaces session quota windows. If a source returns only a weekly window for a pool, OpenUsage does not fill the missing 5h window from another source or an older session. Other metered pools show only windows they actually report. Session quota fallback is labeled as session data and may be older than a live response.

The base URL for the live endpoint is, in order: `acct.BaseURL` → `extra.chatgpt_base_url` → the value parsed from `~/.codex/config.toml` (`chatgpt_base_url`) → `https://chatgpt.com/backend-api`. The path is `/wham/usage` for `chatgpt.com/backend-api` and `/api/codex/usage` otherwise.

### Latest session

- Source: the newest usable `~/.codex/sessions/**/*.jsonl` or `*.jsonl.zst`, ordered by the rollout filename timestamp (mtime is a fallback for other names). The last `token_count` of each checked file is cached until the file's size or mtime changes. The provider skips recent stub files without `token_count` events and parses the trailing turn's `Info.TotalTokenUsage` for tokens, plus `model` and `client` from the same payload.
- Transform: tokens stored as `latest_session_tokens`, model/client stored under `Raw["latest_session_model"]` and `Raw["latest_session_client"]`.

### Daily / model / client breakdowns

- Source: the same session files, read incrementally. Each file's aggregate is cached with the byte offset it was parsed up to:
  - Files whose size and mtime are unchanged are skipped.
  - A plain `.jsonl` that grew is parsed only from the cached offset. An incomplete last line (still being written) is left for the next poll.
  - A file that shrank, was replaced, or was rewritten in place is parsed again in full.
  - Compressed `.jsonl.zst` rollouts can't be resumed at an offset, so a changed one is decoded again in full. Finished rollouts don't change, so each is decoded once.
  - If a rollout exists as both `.jsonl` and `.jsonl.zst` (while Codex compresses it), only the plain file is read, so the session isn't counted twice.
  - Lines the breakdown never uses (compacted history, chat messages, reasoning) are skipped by a byte-prefix check without JSON decoding.

  The first poll after startup parses the full history. Later polls cost milliseconds, even with gigabytes of history. Cost is re-computed from the cached per-turn usage when the pricing tables reload.
- Transform: each turn becomes a usage record. Records are aggregated by model, by client, and by day. Outputs:
  - `sessions_today` — distinct sessions with at least one turn whose timestamp falls in today (local time).
  - Per-model rows with input/output/cached token totals.
  - Per-client rows with the same totals plus session count.

#### How the model is resolved

The model credited to each turn is resolved in this order, first match wins:

1. `model` / `model_id` on the `token_count` event itself (per-turn override).
2. `model` / `model_id` on the `turn_context` line, if present.
3. `model` / `model_id` in the `session_meta` header.
4. `base_instructions.provenance.model` in the `session_meta` header.

Step 4 matters on Codex CLI 0.147.0 and later, which stopped writing an
explicit model to the session header and no longer emits `turn_context` lines
at all. Without it every turn falls through to the `unknown` bucket, which
also zeroes that bucket's cost.

:::note Turns that report only a token total
Some Codex clients emit `token_count` events with `total_tokens` populated but
`input_tokens` and `output_tokens` both zero. Those turns still contribute to
`model_<name>_total_tokens`, but no cost is derived for them — input and output
price differently, so a total alone cannot be converted to spend.
:::

### Rate-limit windows (`rate_limit_primary`, `rate_limit_secondary`)

- Source: app-server `primary`, `secondary`, and `rateLimitsByLimitId`, the HTTP usage response, or the selected local session. Each window reports a used percentage, duration, and optional reset time.
- Transform: `Used = used_percent`, `Remaining = 100 - Used`, `Limit = 100`. The main pool keeps `rate_limit_primary` and `rate_limit_secondary`; other pools use `rate_limit_<id>_<slot>`. Reset times populate `Resets[…]`. The compatibility aliases `plan_auto_percent_used`, `plan_api_percent_used`, and `plan_percent_used` remain available.
- Shared forecast: snapshot normalization also emits `quota_burn_rate` and `quota_runout_hours` from the most restrictive valid quota window. These are provider-neutral metrics used by status surfaces such as the dynamic menu-bar overlay.

### Credit balance

- Source: `credits.balance` (or `credits.has_credits` boolean) from the app-server, HTTP fallback, or local session.
- Transform: stored as a metric `Remaining` in USD. `unlimited=true` is reflected as a special attribute.

### Individual credits and forecast

- Source: `individualLimit` from the Codex CLI app-server `account/rateLimits/read` response. The response provides the current-period `limit`, cumulative `used` credits (or a remaining percentage), and the next `resetsAt` timestamp.
- Transform: `codex_credit_limit` contains used/remaining/total credits, while `codex_credit_percent_used` drives the primary dashboard gauge.
- Personal cap: when `credit_limit_override` is lower than the reported quota, `codex_credit_limit` becomes the effective advisory limit and `codex_credit_reported_limit` retains the authoritative quota. Usage remains the authoritative cumulative amount.
- Forecast: when daily account history is available, OpenUsage calculates the average from observed calendar-day totals, including explicit zero-usage days. That average drives the credit rate, expected credits at reset, projected reserve/deficit at reset, and runout estimate. Today's total is always anchored to the live cumulative account usage. Without daily history, it falls back to cumulative usage divided by elapsed time since the inferred period start, then to successive observed quota samples.
- Forecast source is recorded as `account_daily_history`, `inferred_period_start`, or `observed_usage` so the estimate is distinguishable from authoritative quota data.
- The forecast is an estimate at the current average pace. The reset timestamp and reset countdown are authoritative; if the projected exhaustion falls after reset, surfaces should report that it will not exhaust before the reset rather than presenting a misleading exhaustion time.

### Plan, version, account email

- Source: `plan_type` from the app-server, HTTP fallback, or local session; email from HTTP or auth metadata; CLI version from `~/.codex/version.json`; account ID from `auth.json` (`tokens.account_id` or top-level `account_id`).
- Transform: each stored as a snapshot attribute.

### Patch stats

- Source: scanning JSONL turns for tool-call entries that look like file edits.
- Transform: aggregated counts of patches/files-changed.

### Auth status

- Source: HTTP status code when the fallback call is attempted.
- Transform: `401`/`403` is recorded as a diagnostic. Without usable local or app-server data, the snapshot reports authentication required; otherwise the local-data path stays available.

### What's NOT tracked

- **Per-token spend in dollars from local sessions.** Codex sessions don't carry pricing — only token counts. The credit balance is the only $ figure, and it comes from the live endpoint.
- **Hook-driven real-time events without the integration.** Install the `codex` integration (see [Daemon integrations](../daemon/integrations.md)) for per-turn events.

:::note Cost values hidden by default on Plus / Pro / Team / Enterprise
On a ChatGPT subscription plan (Plus, Pro, Team, Enterprise) the dollar number is misleading — usage is governed by rate-limit windows, not by per-call pricing. OpenUsage hides cost columns by default whenever the live `plan_type` reports a subscription tier; rate-limit windows, sessions, and tokens stay visible. Override with [`dashboard.hide_costs`](../reference/configuration.md#dashboardhide_costs) or the <kbd>c</kbd> keystroke.
:::

### How fresh is the data?

- Polling: every 30 s by default. JSONL files are re-parsed when their mtime/size changes; otherwise served from cache.
- Daily account-credit endpoint: cached in memory per account, with the window driven by the endpoint's own `data_freshness_ts`. Only the historical days of a cached response can go stale — today's total is re-derived from the live cumulative quota on every poll — so the cache asks one question: has the freshness stamp reached the start of today?
  - **Settled** (stamp is at or past today's start, so every earlier day is final): held for 1 hour. The expiry is only a hedge against server-side backfill.
  - **Pending** (the endpoint has not finalised yesterday yet): retried after 5 minutes, so a lagging historical day cannot stay latched and skew the forecast.
  - **Unknown** (no usable `data_freshness_ts` in the response): 15 minutes.
- Daily-endpoint failures are cached too, so a rejected token is not retried on every poll: 30 minutes after a `401`/`403`, 2 minutes after any other failure. The error stays visible in `credit_daily_usage_error` throughout.
- A `200` carrying no usage rows is treated as "no daily history available", not as authoritative account history: the forecast falls back to the elapsed-time estimate and records a `credit_daily_usage` diagnostic rather than claiming `account_daily_history`.
- Hook (when integration is installed): real-time per turn.

## API endpoints used

- Optional live usage endpoint:
  - `GET https://chatgpt.com/backend-api/wham/usage` (default), or
  - `GET <base>/api/codex/usage` for non-ChatGPT bases.
  - Headers: `Authorization: Bearer <auth.json access_token>` and `ChatGPT-Account-Id: <account_id>` when available.
- Optional daily account-credit endpoint:
  - `GET https://chatgpt.com/backend-api/wham/usage/daily-workspace-user-credit-usage` (default), with `start_date`, `end_date`, and `breakdown=product` query parameters.
- Local CLI quota endpoint: `codex app-server --stdio`, using the JSON-RPC handshake followed by `account/rateLimits/read`. On macOS and Linux the app-server runs in its own process group, and the whole group is stopped when the call finishes or times out. An npm-installed `codex` (a Node wrapper that starts the native binary as a child) therefore can't leave an orphaned `codex app-server` behind or stall the daemon's poll.

## Files read

- `~/.codex/sessions/**/*.jsonl` and `*.jsonl.zst` — session transcripts (plain or zstd-compressed)
- `~/.codex/auth.json` — auth token (`tokens.access_token`, `tokens.account_id`)
- `~/.codex/config.toml` — CLI configuration (`chatgpt_base_url` if set)
- `~/.codex/version.json` — installed version

## Caveats

- Individual credit usage and the forecast require authenticated Codex quota data from the live endpoint or CLI app-server; offline sessions still show local activity.
- Rate-limit windows are reported by the API and may differ from documented limits during quota changes.
- The monthly period start is inferred from the next reset because Codex reports the reset boundary but not an explicit start timestamp. Daily account history is an optional authenticated enrichment; transient daily-endpoint failures retain the elapsed-time forecast.
- The provider has hooks-style integration with the daemon: see [Daemon integrations](../daemon/integrations.md).

## Troubleshooting

- **No quota windows** — authenticate with `codex login` and check that the installed CLI supports `codex app-server --stdio`. Local session windows remain available when live requests fail.
- **No credit usage or forecast** — `~/.codex/auth.json` is missing or expired, or the CLI app-server quota request failed. Re-authenticate with the Codex CLI and wait for the next daemon poll.
- **Codex listed by `openusage detect` but missing from the dashboard** (older versions): the daemon could not find the `codex` binary on its service `PATH`. Upgrade, or set `binary` on a manual `codex` account to the full path from `which codex`.
- **Sessions missing** — confirm `sessions_dir` matches the path Codex writes to.

## Related

- [OpenAI](./openai.md) — direct API rate limits for the underlying models
- [Claude Code](./claude-code.md) — sibling local-file coding-agent provider
