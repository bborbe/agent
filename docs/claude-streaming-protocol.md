# Claude streaming-session protocol

The `claude` session implementation in `claude/claude-session.go` holds one
long-lived `claude` process per session and drives it over the CLI's stream-json
protocol on stdio. This document is the protocol that implementation actually
speaks, written down so the knowledge does not live only inside the code.

The Python `ClaudeSDKClient` is the reference for the message shapes, but it is not
importable here: this repository is Go and targets the CLI protocol the SDK wraps.
`claude -p --input-format stream-json --output-format stream-json --verbose` is a
long-lived process that reads JSON turns on stdin and writes JSON events on stdout.

## Launch

The session spawns the process once, on the first turn, with an argument vector —
never through a shell:

```
claude --print --input-format stream-json --output-format stream-json --verbose --strict-mcp-config
```

- `--print` — non-interactive mode; the CLI reads turns and writes events instead of
  opening a terminal UI.
- `--input-format stream-json` — stdin is a stream of newline-delimited JSON turns.
  Without it the CLI reads a single prompt and exits, which is exactly the shape this
  session replaces.
- `--output-format stream-json` — stdout is a stream of newline-delimited JSON
  events, one per line.
- `--verbose` — required alongside `--output-format stream-json`; the CLI refuses the
  combination otherwise. It is also what the existing one-shot runner passes.
- `--strict-mcp-config` — the CLI uses only the MCP servers it was configured with,
  matching the one-shot runner.

Three further flags are conditional:

- `--permission-prompt-tool stdio` — added only when a permission decider is
  configured. It is what makes the CLI raise a tool decision on the stdio control
  channel instead of resolving it itself.
- `--allowedTools <list>` — added only when `ClaudeRunnerConfig.AllowedTools` is
  non-empty.
- `--model <name>` — added only when `ClaudeRunnerConfig.Model` is non-empty.

The session id is deliberately **not** passed. Continuity comes from the held
process, not from `--session-id` or `--resume`, so the argv is identical for every
session id and no id can inject a flag or shell syntax into the command line.

The flag set lives in exactly one function (`sessionArgs`), so a correction against a
pinned CLI version is a one-line change plus an edit to this section. Spec 055's
Assumptions require the exact flag set to be confirmed against the pinned
`@anthropic-ai/claude-code` version and recorded with AC2's result.

## Input turn shape

One JSON object per line on stdin, newline-terminated:

```json
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"<prompt>"}]}}
```

The CLI also accepts a bare string for `content`. The implementation writes the
content-block form, which is the shape the SDK sends and the one that carries
non-text content without a second code path.

## Output event vocabulary

One JSON object per line on stdout. The events the implementation recognises:

| `type` | Carries | Meaning |
|---|---|---|
| `system` | `subtype: "init"`, `session_id` | Startup banner. Ignored by the turn loop. |
| `assistant` | `message.content[]` of `text` and `tool_use` items | Model output mid-turn. Ignored by the turn loop; the turn's answer is the `result` event. |
| `user` | tool results | Tool results echoed back into the stream. Ignored by the turn loop. |
| `result` | `result`, `usage`, `num_turns`, `session_id` | The turn's terminal event. Carries the answer text. |
| `control_request` | `request_id`, `request` | The CLI needs something from the client — a tool decision. See below. |

The parser decodes every line into the same `claudeEvent` vocabulary the one-shot
runner uses, extended in place with the control-request envelope. A line that is not
valid JSON fails the turn loudly: the error records the line's **shape** (its byte
length) and never its content, because turn content is not logged.

An event whose `type` is unrecognised and is **not** a `control_request` is ignored —
that is the forward-compatibility rule for new conversation events. A `control_request`
is never ignored: an unrecognised control-request subtype fails the turn, so a CLI
version that renames the permission request surfaces as a loud error rather than a
silent hang.

## Turn-boundary rule

A turn begins when the implementation writes one input turn to stdin and ends when
the `result` event for that turn is read from stdout. Nothing is read from stdout
outside a turn, and stdout is never read concurrently with a stdin write outside a
single turn.

A permission request raised mid-turn does **not** end the turn. The CLI is blocked
waiting on the answer, so the turn resumes after the response is written to stdin.
The pause is the held process waiting — the implementation does not poll and does not
sleep-loop.

If the turn ends without a `result` event — the context was cancelled, or the process
exited — the turn fails. It is never reported as successful, and the session is
closed so the next turn cannot silently start a fresh conversation.

## Permission-request shape

When the CLI needs a tool decision it writes a request event on stdout:

```json
{"type":"control_request","request_id":"req-1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}
```

`can_use_tool` is the **primary** request subtype the implementation accepts;
`permission` is accepted as the **fallback** spelling. The envelope name
`control_request` is the one the CLI sends.

The implementation builds a `PermissionRequest` from the tool name, the CLI's
description and a bounded preview of the tool input, and asks its
`PermissionDecider` for a verdict. It then writes the verdict back to the same
process's stdin:

```json
{"type":"control_response","request_id":"req-1","response":{"subtype":"success","request_id":"req-1","response":{"behavior":"allow"}}}
```

The response carries the **same `request_id`** the request was raised with, and the
verdict is `behavior: "allow"` or `behavior: "deny"`. The tool-input preview is
carried to the decider and is never logged.

A decider error fails the turn. The implementation never auto-allows, never
auto-denies, and never retries — a permission request raised while the pod's HTTP
surface is unavailable must fail the turn rather than block indefinitely.

## Version confirmation

This vocabulary was implemented against the documented SDK shape, not by probing a
running CLI: the build container has no `claude` binary. The pinned
`@anthropic-ai/claude-code` version and the confirmed flag set are recorded with
spec 055 AC2's result — the operator-executable two-turn probe against the host's
real binary.

A vocabulary change is expected to fail **loudly at the parser** rather than degrade
silently: an unparseable line, or a `control_request` whose subtype is not recognised,
fails the turn with the line's shape recorded. The CLI's idle-timeout behaviour is
likewise surfaced as a per-turn failure — the process exiting mid-turn is reported as
a turn error, not papered over with a fresh conversation.
