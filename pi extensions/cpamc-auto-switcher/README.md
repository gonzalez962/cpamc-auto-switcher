# cpamc-auto-switcher Pi Extension

Pi coding-agent extension that automatically triggers `cpamc-auto-switcher` after each main agent turn or subagent completion.

## Trigger Hooks

1. **Main Agent Turns**: triggers on `turn_end` after the assistant completes a response.
2. **Synchronous Subagents**: triggers on `tool_result` for `subagent_run` or `subagent_continue` in task mode upon terminal status (`completed`, `failed`, etc.).
3. **Background Subagents**: triggers on `message_start` for `subagent-completion` events.

## Behavior

- Runs `cpamc-auto-switcher` asynchronously in the background.
- Emits non-intrusive UI notifications (`ctx.ui.notify`) with the remaining quota or informing when an automatic rotation (`[ROTATED]`) has occurred.
- Serializes invocations using an internal queue to prevent race conditions during credential swaps.
