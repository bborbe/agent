---
status: completed
summary: Add V0 boundary logging to AgentStep.Run and PluginInstaller.ensureOne for external subprocess calls, mirroring pi/pi-step.go pattern
execution_id: repo-exec-208-claude-boundary-logging
dark-factory-version: v0.193.0
created: "2026-08-10T07:29:43Z"
queued: "2026-08-10T07:29:43Z"
started: "2026-08-10T07:35:50Z"
completed: "2026-08-10T07:38:25Z"
---

<summary>
- The `claude` CLI is spawned as a subprocess but its outcome is never logged at default verbosity, so a run that fails or hangs leaves no evidence in the logs.
- The sibling `pi` runner already logs invoke, failure and success with durations at V0. This makes the two boundaries symmetric.
- Plugin installation commands likewise never confirm success, so a silent install looks identical to one that never ran.
- No behaviour changes: only log statements are added.
</summary>

<objective>
Add default-verbosity (`glog.Infof`, i.e. V0) boundary logging for two external-process call sites in `/workspace`, mirroring the pattern already used by `pi/pi-step.go`:
1. the `claude` CLI subprocess invoked via `claude/agent-step.go` → `claude/claude-runner.go`
2. the `claude plugin` / `marketplace` commands in `claude/claude-plugin-installer.go`

Implements code-review findings M1 and M2 (rule `go-logging/external-call-logs-response`).
</objective>

<context>
Read `/workspace/CLAUDE.md` for project conventions (Ginkgo v2 / Gomega, counterfeiter, external test packages, `github.com/bborbe/errors`, glog).

Read these files IN FULL before editing:
- `/workspace/pi/pi-step.go` (237 lines) — **this is the reference implementation.** Lines 45-70 show the exact shape to copy: `runStart := time.Now()` before the call, `glog.Infof` on invoke with prompt size, `glog.Infof` on failure with `time.Since(runStart)` and the error, `glog.Infof` on success with duration and result size.
- `/workspace/claude/agent-step.go` (101 lines) — the caller to change for finding M1. Its `Run` method at line 76 calls `s.cfg.Runner.Run(ctx, prompt)` at line 84 with no surrounding logging.
- `/workspace/claude/claude-runner.go` (301 lines) — note line 107 already has `glog.V(2).Infof("spawning claude CLI: claude %v", args)`. That is a **V2 debug** line and is NOT the outcome log this task adds. Leave it as it is; do not downgrade or duplicate it.
- `/workspace/claude/claude-plugin-installer.go` (150 lines) — the file to change for finding M2. The `ensureOne` / `execPluginCommander.Run` chain runs `claude plugin list`, `marketplace add` and `plugin install`; only some failure paths warn, and no success is ever logged.
</context>

<requirements>
1. In `/workspace/claude/agent-step.go`, in the `Run` method, wrap the `s.cfg.Runner.Run(ctx, prompt)` call at line 84 with V0 logging that mirrors `pi/pi-step.go`:
   - capture `runStart := time.Now()` immediately before the call
   - on the error path (the existing `if runErr != nil` block), `glog.Infof` with the step name (`s.cfg.Name`), `time.Since(runStart)`, and the error — before the existing `return`
   - on the success path, `glog.Infof` with the step name, `time.Since(runStart)`, and the size of `result.Result` in bytes
   - use `glog.Infof` (V0), NOT `glog.V(n).Infof` — this is a low-frequency external-call boundary where every call matters
2. Add the `time` AND `github.com/golang/glog` imports to `/workspace/claude/agent-step.go` if not already present — the file currently imports neither.
3. In `/workspace/claude/claude-plugin-installer.go`, add a `glog.Infof` in `ensureOne` logging the spec name, the action performed, and the resulting error (nil on success) for **every external command invoked in `ensureOne`** — including the two routed through the `runHard` helper (`marketplace add` and `plugin install`, called around lines 116 and 119), not only the paths that currently warn on failure. Logging at the `runHard` call sites satisfies this.
4. Do NOT change any control flow, return values, error wrapping, or existing log lines. This change is additive logging only.
5. Do NOT modify `/workspace/pi/pi-step.go` — it is the reference, not a target.
</requirements>

<verification>
- `cd /workspace && make precommit` exits 0.
- `grep -n 'glog.Infof' /workspace/claude/agent-step.go` shows at least two new lines, one on the failure path and one on the success path, both referencing a duration.
- `grep -n 'time.Since' /workspace/claude/agent-step.go` returns at least two matches.
- `grep -n 'glog.Infof' /workspace/claude/claude-plugin-installer.go` shows log lines inside `ensureOne` covering every external command it invokes, including the two `runHard` call sites.
- `grep -n 'glog.V(2).Infof("spawning claude CLI' /workspace/claude/claude-runner.go` still returns exactly one match — the pre-existing debug line is untouched.
- `cd /workspace && git diff --stat` shows changes ONLY in `claude/agent-step.go` and `claude/claude-plugin-installer.go`.
</verification>

<allowed_files>
- /workspace/claude/agent-step.go
- /workspace/claude/claude-plugin-installer.go
</allowed_files>
