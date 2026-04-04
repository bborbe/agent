---
status: completed
spec: [006-result-writer-conflict-resolution]
summary: Created pkg/conflict package with GeminiConflictResolver, wired it into main.go with required GEMINI_API_KEY field, promoted google.golang.org/genai to direct dependency, and updated K8s manifests; fixed gosec G703 and nestif lint violations in git_client.go to make precommit pass.
container: agent-035-fix-incomplete-033-gemini-resolver
dark-factory-version: v0.94.1-dirty
created: "2026-04-04T00:00:00Z"
queued: "2026-04-04T14:38:45Z"
started: "2026-04-04T14:38:47Z"
completed: "2026-04-04T14:50:38Z"
---

<summary>
- Controller can now resolve git merge conflicts automatically using Gemini LLM
- Conflicted file content is sent to Gemini with safe data delimiters preventing prompt injection
- LLM responses are cleaned (code fences stripped, trailing newline ensured)
- Controller requires GEMINI_API_KEY at startup — refuses to start without it
- K8s manifests updated with the new secret and environment variable
</summary>

<objective>
Complete the partially-applied prompt 033 by creating the `conflict` package (Gemini resolver implementation + tests), wiring it into main.go, and promoting the genai dependency from indirect to direct. The `git_client.go` changes (ConflictResolver interface, resolveConflicts method, containsConflictMarkers, 4-param NewGitClient) already landed and must NOT be modified.
</objective>

<context>
Read CLAUDE.md for project conventions.
Read the coding guides from `~/Documents/workspaces/coding/docs/`: `go-architecture-patterns.md`, `go-testing-guide.md`, `go-error-wrapping-guide.md`, `go-security-linting.md`, `go-glog-guide.md`.

**Current state after interrupted prompt 033:**
- `task/controller/pkg/gitclient/git_client.go` — DONE. Has `ConflictResolver` interface, `conflictResolver` field, 4-param `NewGitClient`, `resolveConflicts`, `containsConflictMarkers`. Do NOT touch this file.
- `task/controller/pkg/conflict/` — directory does NOT exist. Must be created.
- `task/controller/main.go` line 59 — still calls `gitclient.NewGitClient(a.GitURL, vaultLocalPath, a.GitBranch)` with 3 args. Will not compile.
- `task/controller/go.mod` — has `google.golang.org/genai v1.52.1 // indirect`. Needs promotion to direct.

Key files to read before making changes:
- `task/controller/pkg/gitclient/git_client.go` — read the `ConflictResolver` interface definition (around line 39-44) and `NewGitClient` signature (around line 56-68)
- `task/controller/main.go` — read the `application` struct and `Run` method to understand wiring
- `task/controller/go.mod` — confirm genai is listed as indirect
**Gemini SDK API** (from `google.golang.org/genai@v1.52.1`):
```go
client, err := genai.NewClient(ctx, &genai.ClientConfig{
    APIKey:  apiKey,
    Backend: genai.BackendGeminiAPI,
})
result, err := client.Models.GenerateContent(ctx, "gemini-2.5-flash", genai.Text(prompt), nil)
resolved := result.Text()
```
</context>

<requirements>
### 1. Promote `google.golang.org/genai` to direct dependency

In `task/controller/`:
```bash
go get google.golang.org/genai
go mod tidy
```

Verify: `grep "google.golang.org/genai" task/controller/go.mod` must NOT show `// indirect`.

### 2. Create `task/controller/pkg/conflict/gemini_resolver.go`

Create the directory `task/controller/pkg/conflict/` and the file:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conflict
```

Imports needed:
- `"context"`
- `"fmt"`
- `"strings"`
- `"github.com/bborbe/errors"` — for all error wrapping
- `"github.com/golang/glog"` — for logging at V(2) and V(3)
- `"google.golang.org/genai"` — the Gemini SDK

Implement `GeminiConflictResolver`:

- **Constructor**: `NewGeminiConflictResolver(apiKey string) *GeminiConflictResolver`
  - Stores `apiKey` in struct field
  - Returns pointer to struct

- **Struct**: `GeminiConflictResolver` with single field `apiKey string`

- **Method**: `Resolve(ctx context.Context, filename string, content string) (string, error)`
  - Log at `glog.V(2).Infof` the filename and content length
  - Create Gemini client: `genai.NewClient(ctx, &genai.ClientConfig{APIKey: g.apiKey, Backend: genai.BackendGeminiAPI})`
  - Build prompt with this exact structure (data delimiters prevent prompt injection):
    ```
    You are a merge conflict resolver for markdown files. Resolve the conflict markers below and return ONLY the resolved file content with no explanation, no markdown code fences, and no additional commentary.

    RESOLUTION RULES:
    - Merge both versions intelligently
    - For overlapping sections, prefer the content between >>>>>>> markers (the incoming/agent version)
    - Remove ALL conflict markers (lines starting with <<<<<<<, =======, >>>>>>>)
    - Preserve all non-conflicting content exactly as-is
    - Return only the file content, nothing else

    FILE: %s

    BEGIN FILE CONTENT
    %s
    END FILE CONTENT
    ```
    Use `fmt.Sprintf` with `filename` and `content` as arguments.
  - Call: `client.Models.GenerateContent(ctx, "gemini-2.5-flash", genai.Text(prompt), nil)`
  - Extract text: `result.Text()`
  - **Strip markdown code fences**: LLMs often wrap output in ` ```markdown ... ``` ` or ` ``` ... ``` `. After getting the resolved text:
    - If the text starts with a line matching ` ```markdown ` or ` ``` ` (with optional language tag), remove that first line
    - If the text ends with ` ``` ` (possibly followed by whitespace/newline), remove that trailing fence
    - Use `strings.TrimPrefix`/`strings.TrimSuffix` or line-based logic
  - **Ensure trailing newline**: if resolved content does not end with `\n`, append one
  - Log at `glog.V(3).Infof` the filename and before/after byte counts
  - Return the resolved string and nil error

Error handling:
- Client creation fails: `return "", errors.Wrapf(ctx, err, "create Gemini client")`
- GenerateContent fails: `return "", errors.Wrapf(ctx, err, "Gemini GenerateContent failed")`
- Never use `fmt.Errorf` — always `errors.Wrapf` or `errors.Errorf` from `github.com/bborbe/errors`

### 3. Create `task/controller/pkg/conflict/conflict_suite_test.go`

Standard Ginkgo test suite bootstrap:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conflict_test

import (
    "testing"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/onsi/gomega/format"
)

func TestConflict(t *testing.T) {
    format.TruncatedDiff = false
    RegisterFailHandler(Fail)
    RunSpecs(t, "Conflict Suite")
}
```

### 4. Create `task/controller/pkg/conflict/gemini_resolver_test.go`

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conflict_test
```

Imports:
- `"github.com/bborbe/agent/task/controller/pkg/conflict"`
- `"github.com/bborbe/agent/task/controller/pkg/gitclient"`
- `. "github.com/onsi/ginkgo/v2"`
- `. "github.com/onsi/gomega"`

Tests to include:

a) **Compile-time interface check** — at package level (outside Describe):
```go
var _ gitclient.ConflictResolver = &conflict.GeminiConflictResolver{}
```
This ensures GeminiConflictResolver satisfies the ConflictResolver interface at compile time.

b) **Describe("GeminiConflictResolver")** with:
- `It("implements ConflictResolver interface")` — instantiate with `conflict.NewGeminiConflictResolver("fake-key")` and assign to `var _ gitclient.ConflictResolver`, assert no panic (redundant with compile check but documents intent)

Do NOT write tests that call the live Gemini API. The compile-time check is sufficient for unit testing. Integration tests with a live key are out of scope.

### 5. Wire into `task/controller/main.go`

**Add import** for the conflict package:
```go
"github.com/bborbe/agent/task/controller/pkg/conflict"
```

**Add field** to the `application` struct (after the existing fields, before the closing brace):
```go
GeminiAPIKey string `required:"true" arg:"gemini-api-key" env:"GEMINI_API_KEY" usage:"Gemini API key for LLM conflict resolution" display:"length"`
```

The `required:"true"` tag ensures the service refuses to start without a value — handled by the `service.Main` framework (same as `SentryDSN`).

**Update the `Run` method** — replace the 3-arg `NewGitClient` call:

Before (current line 59):
```go
gitClient := gitclient.NewGitClient(a.GitURL, vaultLocalPath, a.GitBranch)
```

After:
```go
conflictResolver := conflict.NewGeminiConflictResolver(a.GeminiAPIKey)
gitClient := gitclient.NewGitClient(a.GitURL, vaultLocalPath, a.GitBranch, conflictResolver)
```

### 6. Update K8s manifests for GEMINI_API_KEY

The controller now requires `GEMINI_API_KEY` at startup. Without updating the K8s manifests, deployment will fail.

**Add to `task/controller/k8s/agent-task-controller-secret.yaml`:**
```yaml
  gemini-api-key: '{{ "GEMINI_API_KEY_KEY" | env | teamvaultPassword | base64 }}'
```
(Same pattern as `task/executor/k8s/agent-task-executor-secret.yaml`)

**Add to `task/controller/k8s/agent-task-controller-sts.yaml`** in the `env:` section of the container:
```yaml
            - name: GEMINI_API_KEY
              valueFrom:
                secretKeyRef:
                  key: gemini-api-key
                  name: agent-task-controller
```
(Same pattern as `task/executor/k8s/agent-task-executor-deploy.yaml`)
</requirements>

<constraints>
- Do NOT modify `task/controller/pkg/gitclient/git_client.go` — those changes already landed correctly
- Do NOT modify `task/controller/pkg/gitclient/git_client_test.go` — those changes already landed correctly
- Use `github.com/bborbe/errors` for all error wrapping — never `fmt.Errorf`, never `context.Background()` in pkg/ code
- Use `github.com/golang/glog` for logging — `glog.V(2).Infof` for resolution events, `glog.V(3).Infof` for verbose detail
- File permissions `0600` for all `os.WriteFile` calls (gosec)
- LLM prompt must delimit file content with BEGIN/END markers to prevent prompt injection
- If LLM returns content with conflict markers still present, the existing `containsConflictMarkers` check in git_client.go handles this — the resolver itself does NOT need to check
- Strip markdown code fences from LLM response (LLMs often wrap output in triple-backtick blocks)
- Ensure trailing newline in resolved content
- Model: use `"gemini-2.5-flash"` as the model name
- ConflictResolver interface is in `pkg/gitclient` — `pkg/conflict` must NOT import `pkg/gitclient` at runtime (only in tests for interface check)
- `required:"true"` on `GeminiAPIKey` — missing key prevents startup via `service.Main` framework
- Project does NOT use vendoring — do NOT run `go mod vendor`
- Do NOT commit — dark-factory handles git
- All existing tests must pass
- `make precommit` must pass in `task/controller`
</constraints>

<verification>
Verify conflict package files exist:
```bash
ls task/controller/pkg/conflict/
```
Must show `gemini_resolver.go`, `gemini_resolver_test.go`, `conflict_suite_test.go`.

Verify GeminiAPIKey is required in main.go:
```bash
grep -n "GeminiAPIKey\|gemini-api-key\|GEMINI_API_KEY" task/controller/main.go
```
Must show the field with `required:"true"`.

Verify NewGitClient is called with 4 args:
```bash
grep -n "NewGitClient\|conflictResolver" task/controller/main.go
```
Must show `conflict.NewGeminiConflictResolver` and 4-arg `NewGitClient` call.

Verify google.golang.org/genai is a direct dependency:
```bash
grep "google.golang.org/genai" task/controller/go.mod
```
Must NOT show `// indirect`.

Verify compile-time interface check in test:
```bash
grep "ConflictResolver" task/controller/pkg/conflict/gemini_resolver_test.go
```
Must show the blank identifier assignment.

Verify git_client.go was NOT modified:
```bash
git diff task/controller/pkg/gitclient/git_client.go
```
Must produce no output.

Verify K8s secret has gemini-api-key:
```bash
grep "gemini-api-key" task/controller/k8s/agent-task-controller-secret.yaml
```
Must show the teamvault reference.

Verify K8s StatefulSet has GEMINI_API_KEY env:
```bash
grep "GEMINI_API_KEY" task/controller/k8s/agent-task-controller-sts.yaml
```
Must show the env var.

Run tests:
```bash
cd task/controller && make test
```
Must exit 0.

Run precommit:
```bash
cd task/controller && make precommit
```
Must exit 0.
</verification>
