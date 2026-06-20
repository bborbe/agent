---
status: completed
spec: [045-bug-task-controller-filename-collision-idempotency]
summary: Added ErrTaskAlreadyExists sentinel error to lib/command/task package in errors.go with accompanying Ginkgo test in errors_test.go verifying non-nil value, stable message, and errors.Is matchability after bborbe/errors.Wrapf wrapping; updated CHANGELOG.md with Unreleased entry.
container: agent-filename-collision-exec-204-spec-045-export-task-already-exists-sentinel
dark-factory-version: v0.182.0
created: "2026-06-20T15:10:00Z"
queued: "2026-06-20T15:10:40Z"
started: "2026-06-20T15:10:42Z"
completed: "2026-06-20T15:13:14Z"
branch: dark-factory/bug-task-controller-filename-collision-idempotency
---

<summary>
- The shared task-command library gains a single exported error value that means "a task file already occupies this filename".
- Other repositories (notably the recurring-task-creator) can match this error by identity (`errors.Is`) to classify a collision as a benign, expected outcome rather than a real failure.
- The error is a plain stdlib error value so its identity is stable across process and repo boundaries — wrapping does not lose it.
- A documentation comment explains when the controller returns it, so `go doc` surfaces the contract to cross-repo callers.
- No wire-format change and no new behavior ship in this prompt — it only publishes the contract that the next prompt's executor change will use.
</summary>

<objective>
Export a single package-level sentinel error `ErrTaskAlreadyExists` from `lib/command/task` so cross-repo callers can match a filename-collision result via `errors.Is`. This is the cross-repo contract; the executor that returns it ships in prompt 2.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — note especially the sentinel-error rule: declare sentinels with the stdlib `errors` package aliased as `stderrors` (`stderrors "errors"`), NOT with `github.com/bborbe/errors` (the latter is only for wrapping). `errors.Is` matchability requires a stable stdlib value.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments start with the symbol name and are full sentences describing behavior.

Key files to read in full before editing:
- `/workspace/lib/command/task/create-command.go` — the package this sentinel belongs in. Package is `package task`; module is `github.com/bborbe/agent/lib`. Note its import block uses `github.com/bborbe/errors` for wrapping; you will ADD a separate `stderrors "errors"` import to a NEW file (do not edit create-command.go).
- `/workspace/lib/command/task/task_suite_test.go` — the existing Ginkgo suite entry point for this package; the new test goes in a new `*_test.go` file in the same `package task_test`.

Inlined load-bearing facts (verified against source):
- The package declaration is `package task` (file `create-command.go` line 5).
- The lib module path is `github.com/bborbe/agent/lib` (lib/go.mod line 1); the package import path is `github.com/bborbe/agent/lib/command/task`.
- This repo already uses the `stderrors "errors"` alias pattern for stdlib errors in tests (e.g. `lib/claude/claude-plugin-installer_test.go` line 9). Mirror that alias name.
</context>

<requirements>
1. **Create the sentinel file** `/workspace/lib/command/task/errors.go` with the standard license header, `package task`, and a single exported sentinel declared via the stdlib `errors` package (aliased `stderrors`). The full file:

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package task

   import (
   	stderrors "errors"
   )

   // ErrTaskAlreadyExists is returned by the task controller's create-task
   // executor when a task file already occupies the target filename in the vault.
   // The controller writes nothing and the CQRS framework converts this error into
   // a Failure on the result topic. Callers across repositories (e.g. the
   // recurring-task-creator) match it via errors.Is to classify the collision as a
   // benign, expected outcome of replaying a CreateCommand for an already-materialized
   // task, rather than a genuine processing failure.
   var ErrTaskAlreadyExists = stderrors.New("task file already exists at title path")
   ```

   Use `stderrors.New` (Go stdlib `errors`). Do NOT use `github.com/bborbe/errors` to declare the sentinel — that package is for wrapping, and its constructors do not produce a stable comparable value for `errors.Is` across repos.

2. **Write a Ginkgo test** in a new file `/workspace/lib/command/task/errors_test.go` (package `task_test`) that pins the two contract guarantees:
   - The sentinel is non-nil and has the documented message.
   - The sentinel survives wrapping by `github.com/bborbe/errors.Wrapf` and is still matchable via `errors.Is` (this is the exact round-trip the controller and cross-repo callers rely on).

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package task_test

   import (
   	"context"
   	stderrors "errors"

   	"github.com/bborbe/errors"
   	. "github.com/onsi/ginkgo/v2"
   	. "github.com/onsi/gomega"

   	task "github.com/bborbe/agent/lib/command/task"
   )

   var _ = Describe("ErrTaskAlreadyExists", func() {
   	It("is a non-nil sentinel with a stable message", func() {
   		Expect(task.ErrTaskAlreadyExists).NotTo(BeNil())
   		Expect(task.ErrTaskAlreadyExists.Error()).
   			To(Equal("task file already exists at title path"))
   	})

   	It("remains matchable via errors.Is after wrapping with bborbe/errors.Wrapf", func() {
   		wrapped := errors.Wrapf(
   			context.Background(),
   			task.ErrTaskAlreadyExists,
   			"title path %s occupied",
   			"tasks/Some Title.md",
   		)
   		Expect(stderrors.Is(wrapped, task.ErrTaskAlreadyExists)).To(BeTrue())
   	})
   })
   ```

   NOTE: `context.Background()` inside a TEST file is permitted (the pkg/-only ban does not apply to tests). If the existing suite already provides a `ctx`, prefer that.

3. **Confirm no other exported symbol is added.** The ONLY new exported identifier in `lib/command/task` is `ErrTaskAlreadyExists`. Do not add helper functions, types, or constructors.
</requirements>

<constraints>
- Sentinel MUST be declared with the stdlib `errors` package (`stderrors "errors"`), never with `github.com/bborbe/errors`. Matchability via `errors.Is` across repos depends on the stable stdlib value. (Spec Constraint.)
- No new exported symbols in `lib/command/task` other than `ErrTaskAlreadyExists`. (Spec Constraint.)
- No changes to `task.CreateCommand` wire shape — this sentinel is server-side only. (Spec Constraint.)
- Use Ginkgo v2 / Gomega; external test package (`package task_test`); counterfeiter for any mocks (none needed here). (Spec Constraint.)
- Do NOT edit `create-command.go` or any other existing file in the package — add the new `errors.go` and `errors_test.go` files only.
- Do NOT commit — dark-factory handles git.
- All existing tests in `lib/...` must continue to pass.
</constraints>

<verification>
```bash
# AC1 evidence — exactly one declaration of the sentinel
grep -nE 'var ErrTaskAlreadyExists\s*=' /workspace/lib/command/task/*.go
# Must return exactly one match in errors.go
```

```bash
# AC1 evidence — go doc surfaces the symbol with its GoDoc line
cd /workspace/lib && go doc ./command/task ErrTaskAlreadyExists
# Must exit 0 and print the documented sentinel
```

```bash
cd /workspace/lib && make precommit
```
Must exit 0.
</verification>

## DARK-FACTORY-REPORT
```yaml
status: success # or: failed, partial
summary: <one-paragraph description of what changed>
verification:
  command: "cd /workspace/lib && make precommit"
  exitCode: 0
improvements:
  - <category: PROMPT|GUIDE|GLOBAL>: <one-line suggestion>  # or omit if none
```
