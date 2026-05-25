---
status: completed
summary: Fixed security display tags (password for AnthropicAuthToken, length for SentryProxy), added GoDoc to agentName, added package doc to prompts, and fixed errors.Wrapf format string issue
container: agent-exec-174-review-agent-claude-3-fix-docs-security
dark-factory-version: v0.173.0
created: "2026-05-24T11:10:00Z"
queued: "2026-05-25T22:23:09Z"
started: "2026-05-25T23:27:38Z"
completed: "2026-05-25T23:28:45Z"
---

<summary>
- AnthropicAuthToken field uses display:"length" which only hides value length, not the token itself — should use display:"password" for full masking
- SentryProxy has no display tag, exposing any embedded credentials in proxy URLs in process listings
- agentName constant in main.go lacks GoDoc comment
- prompts package lacks package-level documentation
- cmd/run-task/main.go uses errors.Wrapf with a literal format string but passes no format arguments
</summary>

<objective>
Fix documentation gaps and security display-tag issues in agent/claude. After this change: credentials are properly masked in process listings, all exported symbols have documentation, and error calls use correct wrapping.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- agent/claude/main.go — agentName constant at line 41, SentryProxy at line 50, AnthropicAuthToken at line 77
- agent/claude/cmd/run-task/main.go — Wrapf issue at line 79
- agent/claude/pkg/prompts/prompts.go — package doc at line 5
</context>

<requirements>

## 1. Add GoDoc to agentName constant

Read `agent/claude/main.go` before editing.

At line 41, the constant is:
```go
const agentName = "claude-agent"
```

Add a doc comment:
```go
// agentName is the identity string used for Prometheus metric grouping and logging.
const agentName = "claude-agent"
```

## 2. Add display:"password" to AnthropicAuthToken

Read `agent/claude/main.go` before editing.

Find the AnthropicAuthToken field (line ~77). Change:
```go
AnthropicAuthToken string `required:"false" arg:"anthropic-auth-token" env:"ANTHROPIC_AUTH_TOKEN" usage:"Bearer token for ANTHROPIC_BASE_URL" display:"length"`
```
To:
```go
AnthropicAuthToken string `required:"false" arg:"anthropic-auth-token" env:"ANTHROPIC_AUTH_TOKEN" usage:"Bearer token for ANTHROPIC_BASE_URL" display:"password"`
```

Verify the arg library supports `display:"password"`. If it does not compile, fall back to `display:"length"` and note in `## Improvements`.

## 3. Add display tag to SentryProxy

Read `agent/claude/main.go` before editing.

Find SentryProxy field (line ~50). Add display tag:
```go
SentryProxy string `required:"false" arg:"sentry-proxy" env:"SENTRY_PROXY" usage:"Sentry Proxy" display:"length"`
```

## 4. Add package doc to prompts package

Read `agent/claude/pkg/prompts/prompts.go` before editing.

At line 5, change:
```go
package prompts
```
To:
```go
// Package prompts provides embedded prompt fragments for the Claude agent.
package prompts
```

## 5. Fix errors.Wrapf in cmd/run-task/main.go

Read `agent/claude/cmd/run-task/main.go` before editing.

At line 79, find:
```go
return errors.Wrapf(ctx, err, "read task file: %s", a.TaskFilePath)
```

The `%s` here is a literal string in the format template, not a format verb. Change to:
```go
return errors.Wrap(ctx, err, "read task file: "+a.TaskFilePath)
```

Or if the intent was to use formatting:
```go
return errors.Wrapf(ctx, err, "read task file: %s", a.TaskFilePath)
```

Note: Verify whether `a.TaskFilePath` actually contains a `%s` character that would be incorrectly interpolated. If the path could contain format verbs, the string concatenation approach is correct. If `a.TaskFilePath` is guaranteed to not contain `%`, the original may have worked by accident.

## 6. Apply same display tag fixes to cmd/run-task/main.go

Read `agent/claude/cmd/run-task/main.go` before editing.

Apply the same SentryProxy display tag fix and AnthropicAuthToken display fix if those fields exist in this file.

## 7. Run make test then make precommit

```bash
cd agent/claude && make test
```
Expected: exit 0.

```bash
cd agent/claude && make precommit
```
Expected: exit 0.

</requirements>

<constraints>
- Only change files in `agent/claude/`
- Do NOT commit — dark-factory handles git
- Follow project conventions: error wrapping with `github.com/bborbe/errors`, never `fmt.Errorf`
- If display:"password" does not compile, revert to display:"length" and document in ## Improvements
</constraints>

<verification>
cd agent/claude && make precommit
</verification>
