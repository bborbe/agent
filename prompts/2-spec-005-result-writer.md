---
status: created
spec: ["005"]
created: "2026-03-29T20:15:00Z"
branch: dark-factory/agent-result-capture
---

<summary>
- Add shared TaskResultRequest type to lib/ with generic frontmatter map + content string
- Add ResultWriter to task/controller — finds task file by identifier, writes frontmatter + content, commits to git
- task/controller is a dumb writer — agent owns the content transformation
- Unknown tasks are skipped
</summary>

<objective>
Create the vault file mutation layer for spec-005. `task/controller/pkg/result/result_writer.go` takes a `TaskResultRequest` (defined in `lib/`), finds the task's markdown file by identifier, writes the generic frontmatter map and content string to disk, and commits+pushes via the existing GitClient. task/controller is a dumb writer — the agent owns content transformation. This is the business logic core; the CQRS wiring comes in the next prompt.
</objective>

<context>
Read CLAUDE.md for project conventions and all relevant `go-*.md` docs in `/home/node/.claude/docs/`.

Key files to read before making changes:
- `lib/agent_task.go` — existing Task struct; `TaskIdentifier`, `TaskContent`, `domain.TaskStatus`, `domain.TaskPhase` types
- `lib/agent_cdb-schema.go` — `TaskV1SchemaID`; used to understand the lib package structure
- `task/controller/pkg/scanner/vault_scanner.go` — `extractFrontmatter`, `injectTaskIdentifier` helper funcs; use the same YAML frontmatter parsing approach
- `task/controller/pkg/gitclient/git_client.go` — `GitClient` interface; `CommitAndPush`, `Path` methods
- `task/controller/pkg/factory/factory.go` — existing factory pattern for composition
- `task/controller/mocks/` — existing generated mocks to understand mock file naming
</context>

<requirements>
### 1. Add `TaskResultRequest` to `lib/`

Create `lib/agent_task-result-request.go`:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
    "context"

    "github.com/bborbe/validation"
)

// TaskResultRequest is the payload published by an agent when it finishes a task.
// task/controller consumes this from agent-task-v1-request and writes it to the vault file.
// Frontmatter is a generic map — task/controller serializes it to YAML without interpreting individual fields.
// Content is the markdown body after the frontmatter closing delimiter.
// The agent owns the content transformation (status, phase, Result section, etc.).
type TaskResultRequest struct {
    TaskIdentifier TaskIdentifier         `json:"taskIdentifier"`
    Frontmatter    map[string]interface{} `json:"frontmatter"`
    Content        string                 `json:"content"`
}

func (r TaskResultRequest) Validate(ctx context.Context) error {
    return validation.All{
        validation.Name("TaskIdentifier", r.TaskIdentifier),
    }.Validate(ctx)
}

func (r TaskResultRequest) Ptr() *TaskResultRequest {
    return &r
}
```

### 2. Create `task/controller/pkg/result/result_writer.go`

Package `result` — single exported interface `ResultWriter` + private struct `resultWriter`.

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package result

import (
    "context"
    // ... imports
)

//counterfeiter:generate -o ../../mocks/result_writer.go --fake-name FakeResultWriter . ResultWriter

// ResultWriter writes a TaskResultRequest back to the vault task file.
type ResultWriter interface {
    WriteResult(ctx context.Context, req lib.TaskResultRequest) error
}

// NewResultWriter creates a ResultWriter that locates task files in the vault
// and writes the result, committing via gitClient.
func NewResultWriter(
    gitClient gitclient.GitClient,
    taskDir string,
) ResultWriter {
    return &resultWriter{
        gitClient: gitClient,
        taskDir:   taskDir,
    }
}
```

#### `WriteResult` implementation contract:

1. **Find the task file** — walk `gitClient.Path()/taskDir` for all `.md` files. For each file, parse YAML frontmatter (reuse the `extractFrontmatter` helper pattern from `task/controller/pkg/scanner/vault_scanner.go` — copy or extract to a shared location; see note below). Parse `task_identifier` field. If it matches `req.TaskIdentifier`, use that file. If no matching file is found, call `glog.Warningf("task file not found for identifier %s, skipping", req.TaskIdentifier)` and return `nil`.

2. **Write the file** — the agent already owns the content transformation (status, phase, Result section, etc.). task/controller is a dumb writer:
   - Serialize `req.Frontmatter` to YAML using `gopkg.in/yaml.v3` `yaml.Marshal`
   - Reconstruct the file as: `"---\n" + marshaledFrontmatter + "---\n" + req.Content`
   - Write to disk using `os.WriteFile(absPath, newContent, 0600)`

3. **Commit and push** — call `gitClient.CommitAndPush(ctx, fmt.Sprintf("[agent-task-controller] write result for task %s", req.TaskIdentifier))`.

**Note on `extractFrontmatter`:** Do NOT reach into the scanner package from result. Instead, copy the `extractFrontmatter` function into `pkg/result/result_writer.go` as a private function. It is small and self-contained. Only used for finding the task file by `task_identifier` in step 1.

### 3. Create `task/controller/pkg/result/result_writer_test.go`

External test package (`package result_test`). Use Ginkgo/Gomega. Use counterfeiter mock for `gitclient.GitClient` (already exists at `task/controller/mocks/git_client.go`).

Test cases required:

- **Normal write** — creates a temp dir with a task file (with frontmatter containing `task_identifier`), calls `WriteResult` with new frontmatter map and content string, verifies:
  - Written file has YAML frontmatter from `req.Frontmatter` serialized correctly
  - Written file body matches `req.Content`
  - `CommitAndPush` is called once with a message containing the task identifier
- **Overwrite** — calls `WriteResult` twice with different content; verifies the second call fully replaces the file content, `CommitAndPush` is called twice
- **Unknown task identifier** — no matching file; verifies `WriteResult` returns nil and `CommitAndPush` is never called
- **Empty frontmatter** — `req.Frontmatter` is empty map; verifies file is still written with empty frontmatter block
- **Frontmatter with nested values** — `req.Frontmatter` contains lists and nested maps (e.g. `tags: [agent-task]`); verifies YAML serialization is correct

### 4. Create test suite bootstrap `task/controller/pkg/result/result_suite_test.go`

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package result_test

import (
    "testing"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/onsi/gomega/format"
)

//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate

func TestResult(t *testing.T) {
    time.Local = time.UTC
    format.TruncatedDiff = false
    RegisterFailHandler(Fail)
    RunSpecs(t, "Result Suite")
}
```

### 5. Update `CHANGELOG.md`

Add or append to `## Unreleased` in the root `CHANGELOG.md`:

```
- feat: Add TaskResultRequest type to lib/ with generic frontmatter map + content string
- feat: Add ResultWriter to task/controller that writes agent results back to vault task files
```
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT add a consumer or CLI flags yet — that is prompt 3; this prompt is the business logic layer only
- Do NOT modify `task/controller/main.go` in this prompt
- The `agent-task-v1-event` topic and existing task/controller git-to-Kafka sync behavior must not be changed
- task/controller remains the single git writer
- task/executor stays a Job launcher — no changes to task/executor in this prompt
- `TaskResultRequest` goes in `lib/` (not in task/controller) — agents need this type to publish requests
- The counterfeiter annotation `//counterfeiter:generate` must be present on the `ResultWriter` interface so `make generate` can produce a mock
- Use `gopkg.in/yaml.v3` for YAML serialization of `req.Frontmatter` (already a transitive dependency via vault-cli)
- ResultWriter must NOT interpret frontmatter fields — just serialize the map to YAML and write it
- Errors must be wrapped with `errors.Wrapf(ctx, err, "message")` — never `fmt.Errorf`
- Test coverage ≥80% for the new `pkg/result/` package
- Use `os.WriteFile(path, content, 0600)` — file permissions must be 0600 (gosec requirement)
- Do NOT call `context.Background()` in pkg/ code — always thread ctx from the caller
</constraints>

<verification>
```bash
cd task/controller && make test
```
Must exit 0 — all result package tests pass.

```bash
cd task/controller && go test -coverprofile=/tmp/cover.out -mod=vendor ./pkg/result/... && go tool cover -func=/tmp/cover.out
```
Statement coverage for `pkg/result/` must be ≥80%.

```bash
cd task/controller && make precommit
```
Must exit 0.
</verification>
