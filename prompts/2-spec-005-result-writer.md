---
status: created
spec: ["005"]
created: "2026-03-29T20:00:00Z"
branch: dark-factory/agent-result-capture
---

<summary>
- A new `TaskResultCommand` struct is added to `lib` as the shared contract for agent result commands
- A new `TaskResultWriter` interface and implementation are added to `task/controller/pkg/writer/`
- The writer locates a task file in the vault by scanning for a matching `task_identifier` in frontmatter
- The writer updates the frontmatter `status` and `phase` fields in-place
- The writer appends a `## Result` section with output text, links, and ISO 8601 timestamp
- If a `## Result` section already exists, the write is skipped (idempotent)
- If the task file is not found, the writer logs a warning and returns without error
- Content containing bare `---` delimiter lines is sanitized before writing to prevent frontmatter corruption
- Unit tests cover: happy path, unknown task ID, duplicate (idempotent), YAML delimiter sanitization, and missing phase in frontmatter
</summary>

<objective>
Add the `TaskResultCommand` shared type to `lib` and implement the `TaskResultWriter` in `task/controller`. The writer finds a task markdown file by task identifier, updates its frontmatter, and appends a result section. This is the file-writing layer that the Kafka consumer (implemented in the next prompt) will call.
</objective>

<context>
Read CLAUDE.md for project conventions, and all relevant `go-*.md` docs in `/home/node/.claude/docs/`.

Key files to read before making changes:
- `lib/agent_task.go` — Task and TaskIdentifier types
- `lib/agent_cdb-schema.go` — TaskV1SchemaID (for package context)
- `task/controller/pkg/scanner/vault_scanner.go` — how `extractFrontmatter` works, how task files are scanned, `injectTaskIdentifier` pattern
- `task/controller/pkg/gitclient/git_client.go` — GitClient interface (writer does NOT call git — that is the handler's job)
- `task/controller/main.go` — how `vaultLocalPath` and `TaskDir` are used
- `task/controller/pkg/factory/factory.go` — existing wiring pattern
</context>

<requirements>
### 1. Create `lib/agent_task-result-command.go`

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

// TaskResultCommand is the payload published by agents to agent-task-v1-request.
// task/controller consumes this command and writes the result back to the vault task file.
type TaskResultCommand struct {
	TaskIdentifier TaskIdentifier `json:"taskIdentifier"`
	Status         string         `json:"status"`
	Phase          string         `json:"phase"`
	Output         string         `json:"output"`
	Links          []string       `json:"links,omitempty"`
}
```

### 2. Create `task/controller/pkg/writer/task_result_writer.go`

New package `writer` in `task/controller/pkg/writer/`.

#### Interface and constructor

```go
//counterfeiter:generate -o ../../mocks/task_result_writer.go --fake-name FakeTaskResultWriter . TaskResultWriter

// TaskResultWriter writes agent results back to vault task files.
type TaskResultWriter interface {
    // WriteResult finds the task file, updates frontmatter, and appends a ## Result section.
    // Returns (true, nil) if the file was written.
    // Returns (false, nil) if the task was not found (warning logged) or the Result section already exists.
    // Returns (false, error) on I/O failure.
    WriteResult(ctx context.Context, cmd lib.TaskResultCommand) (bool, error)
}

// NewTaskResultWriter creates a TaskResultWriter that scans vaultPath/taskDir for task files.
func NewTaskResultWriter(vaultPath string, taskDir string) TaskResultWriter {
    return &taskResultWriter{
        vaultPath: vaultPath,
        taskDir:   taskDir,
    }
}
```

#### Implementation — finding the task file

The writer scans `filepath.Join(vaultPath, taskDir)` for `.md` files using `fs.WalkDir`. For each file, read its content, extract the frontmatter (using the same logic as `vault_scanner.go`), and check for a `task_identifier` field matching `cmd.TaskIdentifier`. Stop scanning once found.

If no file is found: log a warning `glog.Warningf("task %s not found in vault, skipping result", cmd.TaskIdentifier)` and return `(false, nil)`.

#### Implementation — idempotency check

After finding the task file, check if `## Result` already appears anywhere in the file body (the content after the closing frontmatter `---`). If found: log `glog.V(2).Infof("result already written for task %s, skipping", cmd.TaskIdentifier)` and return `(false, nil)`.

#### Implementation — frontmatter update

Use `regexp` to update `status` and `phase` in the frontmatter string (extracted between the opening and closing `---` delimiters):

- Replace the existing `status:` line: `regexp.MustCompile("(?m)^status:.*$").ReplaceAllString(frontmatter, "status: "+cmd.Status)`
- Replace the existing `phase:` line if present: `regexp.MustCompile("(?m)^phase:.*$").ReplaceAllString(frontmatter, "phase: "+cmd.Phase)`. If `phase:` is absent from the frontmatter, append `"phase: " + cmd.Phase` as a new line before the closing `---`.

Reconstruct the file: `"---\n" + updatedFrontmatter + "\n---\n" + body`

where `body` is everything after the closing `---\n` of the original frontmatter.

#### Implementation — sanitizing output content

Before appending the Result section, sanitize `cmd.Output` to prevent YAML frontmatter delimiter confusion:
- Replace any line that is exactly `---` (three dashes, nothing else) with `\---` (backslash-prefixed).
- Use `regexp.MustCompile("(?m)^---$").ReplaceAllString(cmd.Output, `\---`)`.

#### Implementation — appending the Result section

After the updated frontmatter and body, append:

```
\n## Result\n\n**Completed:** <ISO8601 timestamp>\n\n<sanitized output>\n
```

If `cmd.Links` is non-empty, append after the output:

```
\n**Links:**\n- <link1>\n- <link2>\n
```

Use `time.Now().UTC().Format(time.RFC3339)` for the timestamp.

#### Complete file reconstruction

The final file bytes written to disk:

```
---
<updated frontmatter>
---
<original body (everything after original closing ---)>

## Result

**Completed:** <timestamp>

<sanitized output>

**Links:**
- <link1>
...
```

Write to the same `absPath` using `os.WriteFile(absPath, newContent, 0600)`.

Return `(true, nil)` on success.

#### Helper: split frontmatter and body

Use this approach (mirrors `extractFrontmatter` in vault_scanner.go but also returns the body):

1. Content must start with `---\n` or `---\r\n`.
2. Find the closing `\n---` after the opening.
3. `frontmatter` = content between opening and closing delimiters.
4. `body` = everything after the closing `---` line (skip the `\n` after `\n---`).

Handle both `\r\n` and `\n` line endings consistently (use `strings.ReplaceAll(s, "\r\n", "\n")` before processing).

### 3. Create `task/controller/pkg/writer/writer_suite_test.go`

Standard Ginkgo bootstrap:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package writer_test

import (
    "testing"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/onsi/gomega/format"
)

//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate
func TestSuite(t *testing.T) {
    time.Local = time.UTC
    format.TruncatedDiff = false
    RegisterFailHandler(Fail)
    RunSpecs(t, "Writer Suite")
}
```

### 4. Create `task/controller/pkg/writer/task_result_writer_test.go`

Tests must use `os.MkdirTemp` to create a temporary vault directory with a task file. Each test case writes a `.md` file under `tmpDir/24 Tasks/` with YAML frontmatter containing `task_identifier`.

Cover these cases:

**Happy path:**
- File exists, no existing Result section
- Verify: file content after WriteResult contains updated `status:` and `phase:` in frontmatter, contains `## Result`, contains the output text, contains `**Completed:**`, returns `(true, nil)`

**Idempotent — Result section already exists:**
- File exists and already has `## Result` in body
- Verify: WriteResult returns `(false, nil)`, file content is unchanged

**Unknown task ID:**
- No file in vault matches the given TaskIdentifier
- Verify: WriteResult returns `(false, nil)`, no error

**Sanitization of `---` delimiters in output:**
- cmd.Output contains a line that is exactly `---`
- Verify: written file contains `\---` instead of `---` in the Result section

**Missing phase in frontmatter:**
- Frontmatter contains `status` but no `phase` field
- Verify: after WriteResult, frontmatter contains `phase: <value>` from cmd.Phase

**Links rendered correctly:**
- cmd.Links is `["https://example.com/a", "https://example.com/b"]`
- Verify: written file contains `**Links:**\n- https://example.com/a\n- https://example.com/b`

**Error path — file not writable:**
- After finding the task file, make it read-only with `os.Chmod(absPath, 0400)`
- Verify: WriteResult returns `(false, error)` (non-nil error)

### 5. Add counterfeiter annotation to `task/controller/mocks/mocks.go`

Add the following `//go:generate` line so `make generate` creates the mock:

```go
//counterfeiter:generate -o task_result_writer.go --fake-name FakeTaskResultWriter github.com/bborbe/agent/task/controller/pkg/writer.TaskResultWriter
```

Note: the annotation in `task_result_writer.go` is the canonical source — this entry in `mocks/mocks.go` is a secondary registry. Add the annotation directly in `task_result_writer.go` as shown in step 2 above. Do NOT add it to `mocks/mocks.go` separately — the `counterfeiter:generate` comment in the source file is sufficient.

Actually, follow the existing pattern in `mocks/mocks.go` — read that file to understand how other mocks are registered there, and add the TaskResultWriter annotation consistently.

### 6. Update CHANGELOG.md

Append to `## Unreleased` in the root `CHANGELOG.md`:

```
- feat: Add TaskResultCommand type to lib as shared contract for agent result commands
- feat: Add TaskResultWriter to task/controller for writing agent results back to vault task files
```
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT call `gitClient.CommitAndPush` from the writer — the handler (next prompt) is responsible for git operations
- Do NOT implement any Kafka consumer in this prompt — that is the next prompt
- The `TaskResultWriter` interface must use the `//counterfeiter:generate` annotation for mock generation
- The writer must NOT import `task/controller/pkg/gitclient` — it only does file I/O
- `WriteResult` must return `(bool, error)` exactly — bool signals whether the file was written (true) or skipped (false)
- The `task_identifier` field in YAML frontmatter is always lowercase with underscore — match it case-sensitively
- Use `gopkg.in/yaml.v3` for frontmatter parsing (already in go.mod via vault-cli dependency)
- Use `os.WriteFile(path, content, 0600)` for writing files — matches the existing permission pattern
- The `## Result` section heading must appear on its own line with exactly two `#` characters
- Do NOT change `lib/agent_task.go` or `lib/agent_cdb-schema.go`
- Do NOT change `task/controller/pkg/scanner/vault_scanner.go`
- Existing tests must still pass
</constraints>

<verification>
Run lib tests:
```bash
cd lib && make test
```
Must exit 0.

Run writer unit tests:
```bash
cd task/controller && go test ./pkg/writer/...
```
Must exit 0 with all cases passing.

Run full precommit for task/controller:
```bash
cd task/controller && make precommit
```
Must exit 0.
</verification>
