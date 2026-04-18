---
status: committing
summary: 'Applied five code-review cleanups: deleted unused lib/claude/task-content.go, normalized errors.Wrapf to errors.Wrap in three lib validators, injected CurrentDateTimeGetter into CreateKafkaResultDeliverer, gated all glog calls to V(2).Infof in log-tool-use.go, and reordered ClaudeModel type above its constants; make precommit exits 0 in both lib/ and agent/claude/ (osv-scanner failure in github.com/containerd/containerd is pre-existing on unmodified tree).'
container: agent-053-code-review-fixes-lib-and-claude
dark-factory-version: v0.125.1
created: "2026-04-18T19:49:11Z"
queued: "2026-04-18T19:49:11Z"
started: "2026-04-18T20:19:44Z"
completed: "2026-04-18T20:05:27Z"
lastFailReason: 'validate completion report: completion report status: partial'
---
<summary>
- Deletes an unused duplicate `TaskContent` type from the `lib/claude` package (shadow of the canonical `lib.TaskContent`)
- Normalizes `errors.Wrapf` -> `errors.Wrap` in three validation helpers where no format verbs are used
- Makes the `CreateKafkaResultDeliverer` factory testable by injecting `CurrentDateTimeGetter` instead of constructing it inside the factory
- Fixes an inconsistent verbosity gate in the Claude tool-use logger so every log line inside the V(2) guard is itself at V(2)
- Reorders a type declaration so `type ClaudeModel string` appears above its constants
- No behavior change end to end; narrowly scoped code-review cleanups
- Must leave `make precommit` green in both `lib/` and `agent/claude/` modules
</summary>

<objective>
Apply five small code-review fixes in the `agent` repo across the `lib/` module
(including `lib/claude`) and the `agent/claude` binary: remove a duplicate
`TaskContent` type, replace misused `errors.Wrapf` with `errors.Wrap`, inject a
`CurrentDateTimeGetter` into `CreateKafkaResultDeliverer`, tighten verbosity
gating in `log-tool-use.go`, and reorder `ClaudeModel` above its constants.
No behavior change; scope is mechanical cleanup only.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for the Definition of Done.

Key files (repo-relative to `~/Documents/workspaces/agent`):

- `lib/claude/task-content.go` — dead code, duplicate of `lib.TaskContent`. To delete.
- `lib/agent_task-content.go` — canonical `lib.TaskContent` (imported via module path `github.com/bborbe/agent/lib`). Also uses `errors.Wrapf` on a literal message — switch to `errors.Wrap`.
- `lib/agent_task-identifier.go` — `TaskIdentifier.Validate` uses `errors.Wrapf` on a literal message.
- `lib/agent_task-assignee.go` — `TaskAssignee.Validate` uses `errors.Wrapf` on a literal message.
- `lib/claude/types.go` — existing pattern for re-exporting identifiers from sibling packages via type aliases; reference for style but NOT needed for this prompt because `claude.TaskContent` has zero consumers.
- `lib/claude/log-tool-use.go` — verbosity guard bug: guarded block starts with `if !bool(glog.V(2))` but inner calls use `glog.Infof` (which would log unconditionally were it reached from any other code path). Switch to `glog.V(2).Infof` for consistency and to be robust against future refactors that remove the outer guard.
- `lib/claude/claude-model.go` — `const` block is above the `type ClaudeModel string` declaration. Reorder so type precedes constants.
- `agent/claude/pkg/factory/factory.go` — `CreateKafkaResultDeliverer` calls `libtime.NewCurrentDateTime()` internally. Inject `libtime.CurrentDateTimeGetter` as a new parameter instead.
- `agent/claude/main.go` — sole caller of `CreateKafkaResultDeliverer` (`createDeliverer` method, lines ~95-121). Must be updated to construct and thread the `libtime.CurrentDateTimeGetter`.

Important facts:

- `agent/claude/go.mod` uses a local `replace` directive: `github.com/bborbe/agent/lib => ../../lib`. There is NO lib tag bump needed — the `replace` means the agent/claude module always consumes lib from the workspace. Do NOT add or change any `require`/`replace` lines in `agent/claude/go.mod`.
- `lib/claude/task-content.go` (the file to delete) has ZERO consumers in this repo — `claude.TaskContent` is dead code. Every existing `TaskContent` call site already references `lib.TaskContent` via `github.com/bborbe/agent/lib`. Grep confirms: no file imports `claude.TaskContent` (capital-C Claude package). Deletion is pure removal; no import migrations are required.
- The `github.com/bborbe/errors` package exposes `Wrap(ctx, err, msg string)` and `Wrapf(ctx, err, format string, args ...any)`. When there are no format verbs in the message, `Wrap` is the correct function. The affected sites all pass a plain literal (e.g. `"identifier missing"`, `"content missing"`, `"assignee missing"`) with no format arguments.
- `github.com/bborbe/time` (aliased as `libtime` in the factory) exposes `CurrentDateTimeGetter` interface and `NewCurrentDateTime()` constructor. Match the existing style in the repo for injection.
- `lib/claude/log-tool-use.go` declares `logToolUse` and is called by `lib/claude/claude-runner.go`. The outer `if !bool(glog.V(2)) { return }` guard remains — the fix is inner lines only.
- Repo root has a single top-level `CHANGELOG.md` (no per-module CHANGELOG inside `lib/` or `agent/claude/`). The release process uses the top-level file.
</context>

<requirements>

1. **Delete `lib/claude/task-content.go` entirely.** The file declares an
   unused `claude.TaskContent` type that duplicates the canonical
   `lib.TaskContent`. Remove the file; do NOT add any replacement
   `type TaskContent = lib.TaskContent` alias to `lib/claude/types.go` —
   it has zero consumers in this repo. After deletion, run
   `grep -rn "claude\\.TaskContent\\|claudelib\\.TaskContent" --include="*.go" .`
   from the repo root to confirm zero hits. If any hit appears (new code
   added between audit and execution), replace those call sites with
   `lib.TaskContent` / `agentlib.TaskContent` using the existing import
   path already used in that file (grep the file for `agentlib ` or
   `"github.com/bborbe/agent/lib"`).

2. **Replace `errors.Wrapf` -> `errors.Wrap` in three lib validation
   helpers.** The affected lines wrap a literal string with no format
   verbs, so `Wrap` is the correct call. Use a precise edit that
   preserves all other arguments.

   a. `lib/agent_task-identifier.go` — `TaskIdentifier.Validate`:
      ```go
      // Before
      return errors.Wrapf(ctx, validation.Error, "identifier missing")
      // After
      return errors.Wrap(ctx, validation.Error, "identifier missing")
      ```

   b. `lib/agent_task-content.go` — `TaskContent.Validate`:
      ```go
      // Before
      return errors.Wrapf(ctx, validation.Error, "content missing")
      // After
      return errors.Wrap(ctx, validation.Error, "content missing")
      ```

   c. `lib/agent_task-assignee.go` — `TaskAssignee.Validate`:
      ```go
      // Before
      return errors.Wrapf(ctx, validation.Error, "assignee missing")
      // After
      return errors.Wrap(ctx, validation.Error, "assignee missing")
      ```

3. **Inject `CurrentDateTimeGetter` into `CreateKafkaResultDeliverer`**
   in `agent/claude/pkg/factory/factory.go`.

   - Add a new parameter `currentDateTime libtime.CurrentDateTimeGetter`
     to the function signature, placed at the END of the parameter list
     (after `taskContent string`).
   - Replace the in-body call `libtime.NewCurrentDateTime()` with the
     injected `currentDateTime` parameter.
   - Keep the `libtime "github.com/bborbe/time"` import (still needed
     for the parameter type). Do NOT remove the import.
   - Resulting function body:
     ```go
     // CreateKafkaResultDeliverer creates a ResultDeliverer that publishes task updates to Kafka.
     func CreateKafkaResultDeliverer(
         syncProducer libkafka.SyncProducer,
         branch base.Branch,
         taskID agentlib.TaskIdentifier,
         taskContent string,
         currentDateTime libtime.CurrentDateTimeGetter,
     ) claudelib.ResultDeliverer {
         return claudelib.NewResultDelivererAdapter(
             delivery.NewKafkaResultDeliverer(
                 syncProducer,
                 branch,
                 taskID,
                 taskContent,
                 delivery.NewFallbackContentGenerator(),
                 currentDateTime,
             ),
         )
     }
     ```

4. **Thread `CurrentDateTimeGetter` from `agent/claude/main.go`.**
   Update the sole caller — `(*application).createDeliverer(ctx)` around
   the `factory.CreateKafkaResultDeliverer(...)` call (currently lines
   ~107-112). Add a new argument after `a.TaskContent`:
   `libtime.NewCurrentDateTime()`. Add the import
   `libtime "github.com/bborbe/time"` to `agent/claude/main.go` (use the
   same alias as the factory file for consistency; if `libtime` alias
   collides with an existing import, use the repo's convention from the
   factory file exactly).
   Resulting call:
   ```go
   deliverer := factory.CreateKafkaResultDeliverer(
       syncProducer,
       a.Branch,
       taskID,
       a.TaskContent,
       libtime.NewCurrentDateTime(),
   )
   ```
   Note: `agent/claude/main.go` currently does NOT import
   `github.com/bborbe/time` (confirmed via grep). Add the import fresh
   under the `libtime` alias; no existing alias collision.

5. **Fix the verbosity gate in `lib/claude/log-tool-use.go`.** Inside
   the `logToolUse` function body, every call `glog.Infof(...)` must
   become `glog.V(2).Infof(...)`. This affects every line from
   `glog.Infof("claude: [%s]", c.Name)` through the `default:` branch
   (~lines 19-37 today). The outer `if !bool(glog.V(2)) { return }`
   guard stays.

   Resulting logger after the edit (illustrative, preserve all existing
   format strings and arguments verbatim):
   ```go
   func logToolUse(c claudeContent) {
       if !bool(glog.V(2)) {
           return
       }
       var inp map[string]any
       if err := json.Unmarshal(c.Input, &inp); err != nil {
           glog.V(2).Infof("claude: [%s]", c.Name)
           return
       }

       switch c.Name {
       case "Read":
           glog.V(2).Infof("claude: [read] %v", inp["file_path"])
       case "Write":
           glog.V(2).Infof("claude: [write] %v", inp["file_path"])
       case "Edit":
           glog.V(2).Infof("claude: [edit] %v", inp["file_path"])
       case "Grep":
           glog.V(2).Infof("claude: [grep] %v", inp["pattern"])
       case "Glob":
           glog.V(2).Infof("claude: [glob] %v", inp["pattern"])
       case "Bash":
           glog.V(2).Infof("claude: [bash] %v", inp["command"])
       default:
           glog.V(2).Infof("claude: [%s] %v", c.Name, inp)
       }
   }
   ```

6. **Reorder `lib/claude/claude-model.go`** so `type ClaudeModel string`
   precedes the constants, AND merge the two constants into a single
   grouped `const ( ... )` block (gofumpt/gofmtgroup-style). Target
   layout:
   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package claude

   // ClaudeModel identifies which Claude model to use.
   type ClaudeModel string

   // String returns the model name.
   func (c ClaudeModel) String() string { return string(c) }

   const (
       SonnetClaudeModel ClaudeModel = "sonnet"
       OpusClaudeModel   ClaudeModel = "opus"
   )
   ```
   Preserve all doc comments and the copyright header verbatim.

7. **Update `CHANGELOG.md`** (the top-level file at repo root).

   First read the file's top section to find the highest-versioned
   heading (e.g. `## v0.42.0` or whatever the current top release
   header is). If there is no existing `## Unreleased` section, create
   one IMMEDIATELY above that top release heading. If `## Unreleased`
   already exists, APPEND the bullets below to the existing section.
   Use these exact bullets (terse; /commit will release on master merge):

   ```
   ## Unreleased

   - chore: remove unused duplicate `lib/claude.TaskContent` type (use `lib.TaskContent`)
   - refactor: replace `errors.Wrapf` with `errors.Wrap` in lib validation helpers (no format verbs)
   - refactor: inject `CurrentDateTimeGetter` into `CreateKafkaResultDeliverer` factory for testability
   - fix: use `glog.V(2).Infof` consistently inside the V(2)-guarded block in `lib/claude/log-tool-use.go`
   - chore: reorder `ClaudeModel` type above its constants
   ```

8. **Do NOT modify** (out of scope for this prompt):
   - Any file under `task/controller/`, `task/executor/`, `agent/` (other than the two named files), `lib/delivery/`, `lib/mocks/`.
   - `go.mod` / `go.sum` / `vendor/` in any module.
   - Any counterfeiter mock. The function signature change on `CreateKafkaResultDeliverer` does NOT cross an interface boundary (factory functions are not mocked).
   - No lib version tag bump; no `require`/`replace` changes in `agent/claude/go.mod`.

</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Do NOT bump Go version or module dependencies.
- If `make precommit` fails ONLY on `osv-scanner` with a build error in
  an indirect dependency (e.g. `github.com/containerd/containerd`), that
  is a transient / pre-existing infra issue unrelated to this prompt.
  Confirm by `git stash && make osv-scanner && git stash pop`; if the
  unmodified tree has the same failure, treat the change set as
  complete and report `"status":"success"` (cite the pre-existing
  issue in the summary). The five scoped edits do not touch vendored
  code, go.mod, or any osv-scanner input.
- Do NOT change any CRD type, interface signature, or exported identifier outside the five scoped changes above.
- Use `github.com/bborbe/errors` for any new error wrapping — never `fmt.Errorf`.
- All new exported identifiers (none expected in this prompt) need doc comments.
- Preserve existing license headers and file-scope comments verbatim.
- Keep imports in the existing groupings and aliases (`libtime`, `libkafka`, `agentlib`, `claudelib`, `delivery`).
- Scope is `lib/` and `agent/claude/` only. No cross-repo changes. No controller or executor changes.
</constraints>

<verification>
Run precommit from both affected modules — both must exit 0:

```bash
cd lib && make precommit
```

```bash
cd agent/claude && make precommit
```

Verify the duplicate type was removed:

```bash
grep -rn "type TaskContent\b" lib/claude/
```
Must return nothing (zero lines). The canonical `lib.TaskContent` remains.

Verify `errors.Wrapf` was purged from the three target files:

```bash
grep -E "errors\\.Wrapf" lib/agent_task-identifier.go lib/agent_task-content.go lib/agent_task-assignee.go
```
Must return nothing.

Verify the factory no longer calls `NewCurrentDateTime` internally:

```bash
grep "libtime.NewCurrentDateTime" agent/claude/pkg/factory/factory.go
```
Must return nothing.

Verify the factory accepts the injected dependency:

```bash
grep -n "CurrentDateTimeGetter" agent/claude/pkg/factory/factory.go
```
Must show the new parameter on `CreateKafkaResultDeliverer`.

Verify main.go threads the dependency:

```bash
grep -n "NewCurrentDateTime" agent/claude/main.go
```
Must show exactly one hit inside `createDeliverer`.

Verify every `glog.Infof` inside the tool-use logger was gated to V(2):

```bash
grep "glog.Infof" lib/claude/log-tool-use.go
```
Must return nothing (every line is now `glog.V(2).Infof`).

Verify the type ordering:

```bash
grep -n "type ClaudeModel\\|const.*ClaudeModel" lib/claude/claude-model.go
```
The line number for `type ClaudeModel string` must be SMALLER than the
line numbers for the `SonnetClaudeModel` / `OpusClaudeModel` constants.

Verify the CHANGELOG:

```bash
grep -n -A6 "^## Unreleased" CHANGELOG.md | head -12
```
Must show the five new bullets immediately above `## v0.40.0`.
</verification>
