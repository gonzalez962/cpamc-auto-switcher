# cpamc-auto-switcher Pi Extension

Pi coding-agent extension that automatically triggers `cpamc-auto-switcher` after agent turns, subagent completions, and on real-time rate limit (HTTP 429) or quota exhaustion events.

## Trigger Hooks & Events

The extension monitors multiple lifecycle hooks across the main agent and subagents, executing `cpamc-auto-switcher` in either normal or forced (`--force`) mode:

### 1. HTTP 429 Rate Limit Detection (`after_provider_response`)
- **Hook**: `after_provider_response`
- **Condition**: Triggers when the provider returns an HTTP status `429` (Too Many Requests).
- **Execution**: Dispatches an immediate rotation check with `--force`.
- **UI**: Emits an immediate warning notification (`⚡ Rate limit (429) detectado en proveedor. Ejecutando cambio forzado de cuenta...`).
- **Timing**: Catches rate limits early at the network/response level before stream completion.

### 2. Message Error & Turn End Quota Errors (`message_end` & `turn_end`)
- **`message_end`**:
  - Triggers when an assistant message finishes with `stopReason === "error"`.
  - Inspects `errorMessage` for rate limit or quota patterns (`429`, `rate limit`, `too many requests`, `quota`, `resource exhausted`).
  - When detected, emits a warning notification and schedules rotation with `--force`.
- **`turn_end`**:
  - Triggers after every main agent assistant turn.
  - If the turn ended due to a 429 or quota error (`stopReason === "error"`), schedules an immediate rotation with `--force`.
  - For standard successful or non-quota turns, schedules a routine rotation check (normal mode, respecting cooldown).

### 3. Subagent Error & Quota Inspection
- **Synchronous Subagents (`tool_result`)**:
  - Listens for `tool_result` events from `subagent_run` and `subagent_continue` upon reaching terminal status (`completed`, `failed`, `cancelled`, `interrupted`).
  - Evaluates `task.error` or `task.result` against quota and rate limit error patterns when `task.status === "failed"`.
  - If a rate limit or quota exhaustion is detected, schedules rotation with `--force`; otherwise triggers routine evaluation.
- **Background Subagents (`message_start`)**:
  - Listens for `message_start` events with `customType === "subagent-completion"`.
  - Evaluates terminal background tasks for rate limit and quota exhaustion patterns.
  - Triggers `--force` rotation if rate limit or quota failure occurred.

## `--force` Execution & Cooldown Bypass

By default, `cpamc-auto-switcher` enforces a cooldown period (default 5 minutes) between quota evaluations to avoid excessive API calls to CLIProxyAPI.

### Why `--force` Bypasses Cooldown
When an active account hits an HTTP 429 or quota exhaustion error:
- The active account is depleted or blocked and cannot serve subsequent prompts or subagent calls.
- The AI client harness or subagent framework will often attempt backoff retries.
- Running with `--force` instructs `cpamc-auto-switcher` to bypass the cooldown timer immediately.
- This ensures the depleted active account prefix is rotated to a healthy reserve account **before** retry backoff completes, enabling automated, zero-downtime recovery without human intervention.

### Debounce Mechanism (3 Seconds)
- When a 429 or quota limit occurs, multiple hooks may fire in rapid succession (e.g. `after_provider_response` followed immediately by `message_end` and `turn_end`, or parallel subagents failing simultaneously).
- A 3-second debounce window (`FORCED_DEBOUNCE_MS = 3_000`) prevents redundant, concurrent CLI invocations while ensuring the very first detection triggers immediate account rotation.

## General Behavior

- **Non-blocking Background Execution**: Runs `cpamc-auto-switcher` asynchronously in the background (`windowsHide: true` on Windows to prevent console window flicker).
- **Serialized Execution**: All runs are queued sequentially via an internal promise chain to prevent concurrent race conditions during prefix mutation.
- **UI Notifications**: Emits non-intrusive notifications (`ctx.ui.notify`) displaying remaining quotas or indicating when an automatic rotation (`[ROTATED]`) has succeeded.
