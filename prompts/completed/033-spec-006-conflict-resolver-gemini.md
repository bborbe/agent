---
status: completed
spec: [006-result-writer-conflict-resolution]
container: agent-033-spec-006-conflict-resolver-gemini
dark-factory-version: v0.94.1-dirty
created: "2026-04-04T00:00:00Z"
queued: "2026-04-04T12:18:51Z"
started: "2026-04-04T12:53:05Z"
completed: "2026-04-04T13:09:23Z"
---

<summary>
- When a rebase produces conflict markers, an LLM (Gemini) resolves each conflicted file before retrying the push
- Conflict resolution is a generic markdown merge — the resolver has no knowledge of task structure or frontmatter semantics
- The LLM prompt treats file content as data (clearly delimited), not instructions — preventing prompt injection
- If the LLM returns content still containing conflict markers, the rebase is aborted and the command fails
- If the Gemini API call fails, the rebase is aborted and the command fails with the API error
- The controller refuses to start without a Gemini API key — the missing-key check runs at startup, not at first conflict
- The fast path (push succeeds, no conflict) never calls the Gemini API
- ConflictResolver is an injected interface — gitClient has no direct dependency on the Gemini SDK
</summary>

<objective>
Wire in LLM-based conflict resolution so that the merge conflicts detected after a failed rebase are automatically resolved by Gemini before the push is retried. Requires: a `ConflictResolver` interface injected into `gitClient`, a Gemini implementation in `pkg/conflict/`, a required `GeminiAPIKey` startup flag in `main.go`, and factory wiring. This extends the push-retry logic added in prompt 2 (2-spec-006-push-retry-rebase).
</objective>

<context>
Read CLAUDE.md for project conventions.
Read the coding plugin guides from `~/Documents/workspaces/coding/docs/`: `go-architecture-patterns.md`, `go-testing-guide.md`, `go-factory-pattern.md`, `go-security-linting.md`.

**Prerequisite:** Prompts `spec-006-git-serialization` and `spec-006-push-retry-rebase` have been applied. Before making changes, verify: `grep -n "pushWithRetry\|abortRebase\|conflictedFiles" task/controller/pkg/gitclient/git_client.go` — must show all three methods. If not found, stop and report that prerequisite prompts have not been applied. `gitClient` now has mutex serialization and push-retry-with-rebase. When conflicts are detected, it currently aborts and returns an error. This prompt wires in the LLM resolver before that abort path.

**Gemini SDK:** `google.golang.org/genai` is already an indirect dependency in `task/controller/go.mod`. It needs to be promoted to a direct dependency. After `go get google.golang.org/genai && go mod vendor`, check the vendored source at `task/controller/vendor/google.golang.org/genai/` to confirm exact constructor and `GenerateContent` method signatures before writing the implementation.

**Security requirement:** The LLM prompt must not allow conflict file content to be interpreted as instructions. Wrap content in explicit data delimiters. Example prompt structure:
```
You are a merge conflict resolver for markdown files. Resolve the conflict markers below and return only the resolved file content with no explanation.

CONFLICT INSTRUCTIONS:
- Merge both versions, preferring the content between >>>>>>> (the incoming/agent version) for overlapping sections
- Remove all conflict markers (<<<<<<<, =======, >>>>>>>)
- Do not add any explanation, commentary, or markdown formatting around your answer

FILE: <filename>

BEGIN FILE CONTENT
<conflicted file content>
END FILE CONTENT
```

Key files to read before making changes:
- `task/controller/pkg/gitclient/git_client.go` — current implementation; add `ConflictResolver` interface and inject it
- `task/controller/pkg/gitclient/git_client_test.go` — existing tests; add conflict-resolution test cases
- `task/controller/pkg/factory/factory.go` — `CreateSyncLoop` and `CreateCommandConsumer` factories; does NOT create gitClient itself (that happens in main.go)
- `task/controller/main.go` — creates `gitclient.NewGitClient`; add `GeminiAPIKey` field and pass resolver
- `task/controller/go.mod` — needs `google.golang.org/genai` promoted to direct dep
</context>

<requirements>
### 1. Define `ConflictResolver` interface in the `gitclient` package

In `task/controller/pkg/gitclient/git_client.go`, add:

```go
// ConflictResolver resolves merge conflict markers in a single file's content.
// It receives the filename (for context) and the full file content including conflict markers.
// It returns the resolved content with all conflict markers removed, or an error.
type ConflictResolver interface {
    Resolve(ctx context.Context, filename string, content string) (string, error)
}
```

Add `conflictResolver ConflictResolver` field to `gitClient` struct:
```go
type gitClient struct {
    gitURL           string
    localPath        string
    branch           string
    mu               sync.Mutex
    conflictResolver ConflictResolver // nil means no LLM resolution available
}
```

Update `NewGitClient` to accept `ConflictResolver` as last parameter:
```go
// NewGitClient creates a GitClient that uses the git binary via subprocess.
// conflictResolver is called when a rebase produces merge conflicts; pass nil to disable LLM resolution.
func NewGitClient(gitURL string, localPath string, branch string, conflictResolver ConflictResolver) GitClient {
    return &gitClient{
        gitURL:           gitURL,
        localPath:        localPath,
        branch:           branch,
        conflictResolver: conflictResolver,
    }
}
```

Update the call site in `task/controller/main.go`:
```go
conflictResolver := conflict.NewGeminiConflictResolver(a.GeminiAPIKey)
gitClient := gitclient.NewGitClient(a.GitURL, vaultLocalPath, a.GitBranch, conflictResolver)
```

### 2. Update `pushWithRetry` to call `conflictResolver` when conflicts are present

In `task/controller/pkg/gitclient/git_client.go`, replace the conflict-detected branch inside `pushWithRetry`:

**Before (from prompt 2 — returns error on conflict):**
```go
if len(conflicted) > 0 {
    g.abortRebase(ctx)
    return errors.Errorf(ctx, "rebase produced merge conflicts in %d file(s): %v", len(conflicted), conflicted)
}
```

**After (call resolver, or abort if no resolver):**
```go
if len(conflicted) > 0 {
    if g.conflictResolver == nil {
        g.abortRebase(ctx)
        return errors.Errorf(ctx, "rebase produced merge conflicts in %d file(s) and no conflict resolver is configured: %v", len(conflicted), conflicted)
    }
    if err := g.resolveConflicts(ctx, conflicted); err != nil {
        g.abortRebase(ctx)
        return errors.Wrapf(ctx, err, "conflict resolution failed")
    }
    // After resolution, continue rebase
    // #nosec G204 -- binary is hardcoded "git", args from trusted internal config
    continueCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "rebase", "--continue")
    continueCmd.Env = append(os.Environ(), "GIT_EDITOR=true") // skip editor for commit message
    if out, err := continueCmd.CombinedOutput(); err != nil {
        g.abortRebase(ctx)
        return errors.Wrapf(ctx, err, "git rebase --continue failed: %s", string(out))
    }
    glog.V(2).Infof("conflict resolution complete, retrying push")
    // #nosec G204 -- binary is hardcoded "git", args from trusted internal config
    retryAfterResolve := exec.CommandContext(ctx, "git", "-C", g.localPath, "push")
    if out, err := retryAfterResolve.CombinedOutput(); err != nil {
        return errors.Wrapf(ctx, err, "push after conflict resolution failed: %s", string(out))
    }
    return nil
}
```

### 3. Add `resolveConflicts` private method

```go
// resolveConflicts calls the ConflictResolver for each conflicted file, writes the resolved content,
// and stages the file with `git add`. Must be called with the rebase in-progress (inside the lock).
func (g *gitClient) resolveConflicts(ctx context.Context, conflicted []string) error {
    for _, relPath := range conflicted {
        absPath := filepath.Join(g.localPath, relPath)
        // #nosec G304 -- path constructed from trusted localPath + git-reported conflict list
        contentBytes, err := os.ReadFile(absPath)
        if err != nil {
            return errors.Wrapf(ctx, err, "read conflicted file %s", relPath)
        }
        resolved, err := g.conflictResolver.Resolve(ctx, filepath.Base(relPath), string(contentBytes))
        if err != nil {
            return errors.Wrapf(ctx, err, "LLM resolution failed for %s", relPath)
        }
        // Safety check: resolved content must not contain conflict markers
        if containsConflictMarkers(resolved) {
            return errors.Errorf(ctx, "LLM returned content still containing conflict markers for %s", relPath)
        }
        // #nosec G306 -- 0600 is intentional for task files
        if err := os.WriteFile(absPath, []byte(resolved), 0600); err != nil {
            return errors.Wrapf(ctx, err, "write resolved file %s", relPath)
        }
        // Stage the resolved file
        // #nosec G204 -- binary is hardcoded "git", args from trusted internal config
        addCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "add", relPath)
        if out, err := addCmd.CombinedOutput(); err != nil {
            return errors.Wrapf(ctx, err, "git add resolved file %s: %s", relPath, string(out))
        }
        glog.V(2).Infof("resolved conflict in %s", relPath)
    }
    return nil
}

// containsConflictMarkers returns true if the content contains git conflict markers.
// Checks for line-start anchored markers to avoid false positives from markdown content.
func containsConflictMarkers(content string) bool {
    for _, line := range strings.Split(content, "\n") {
        if strings.HasPrefix(line, "<<<<<<<") ||
            strings.HasPrefix(line, "=======") ||
            strings.HasPrefix(line, ">>>>>>>") {
            return true
        }
    }
    return false
}
```

### 4. Create `task/controller/pkg/conflict/` package

Create `task/controller/pkg/conflict/gemini_resolver.go`:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conflict

import (
    "context"
    "fmt"

    "github.com/bborbe/errors"
    "github.com/golang/glog"
    "google.golang.org/genai"
)

// NewGeminiConflictResolver creates a ConflictResolver backed by the Gemini API.
func NewGeminiConflictResolver(apiKey string) *GeminiConflictResolver {
    return &GeminiConflictResolver{apiKey: apiKey}
}

// GeminiConflictResolver resolves git merge conflicts using the Gemini LLM.
type GeminiConflictResolver struct {
    apiKey string
}

// Resolve sends the conflicted file content to Gemini and returns the resolved content.
// The prompt treats file content as data (delimited), not instructions.
func (g *GeminiConflictResolver) Resolve(ctx context.Context, filename string, content string) (string, error) {
    glog.V(2).Infof("resolving conflict in %s via Gemini (%d bytes)", filename, len(content))

    client, err := genai.NewClient(ctx, &genai.ClientConfig{
        APIKey: g.apiKey,
    })
    if err != nil {
        return "", errors.Wrapf(ctx, err, "create Gemini client")
    }

    prompt := fmt.Sprintf(`You are a merge conflict resolver for markdown files. Resolve the conflict markers below and return ONLY the resolved file content with no explanation, no markdown code fences, and no additional commentary.

RESOLUTION RULES:
- Merge both versions intelligently
- For overlapping sections, prefer the content between >>>>>>> markers (the incoming/agent version)
- Remove ALL conflict markers (lines starting with <<<<<<<, =======, >>>>>>>)
- Preserve all non-conflicting content exactly as-is
- Return only the file content, nothing else

FILE: %s

BEGIN FILE CONTENT
%s
END FILE CONTENT`, filename, content)

    result, err := client.Models.GenerateContent(ctx, "gemini-2.0-flash", genai.Text(prompt), nil)
    if err != nil {
        return "", errors.Wrapf(ctx, err, "Gemini GenerateContent failed")
    }
    resolved := result.Text()
    glog.V(3).Infof("Gemini resolved %s: %d bytes → %d bytes", filename, len(content), len(resolved))
    return resolved, nil
}
```

**Important:** After writing the file, run `go get google.golang.org/genai` in `task/controller/` to promote it to a direct dependency, then `go mod tidy && go mod vendor`. Check the exact Gemini SDK API in `task/controller/vendor/google.golang.org/genai/` before using it — the client constructor and GenerateContent signatures above may need adjustment to match the vendored version.

Create test suite `task/controller/pkg/conflict/conflict_suite_test.go`:
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

Create `task/controller/pkg/conflict/gemini_resolver_test.go`:

Test the `containsConflictMarkers` helper (exported or tested via the package). Since the Gemini API requires a live key, unit tests should use a mock HTTP server or test the non-network parts:
- **Test: conflict marker detection** — test a function that checks for `<<<<<<<`/`=======`/`>>>>>>>` in strings
- **Test: GeminiConflictResolver with mock server** — if you can intercept HTTP calls (via `httptest.NewServer` + custom transport), test that the prompt is built correctly; if not, skip and note that integration testing requires a live API key
- At minimum, verify the package compiles and the struct implements `gitclient.ConflictResolver` interface (compile-time check via blank identifier assignment)

### 5. Add `GeminiAPIKey` to `task/controller/main.go`

Add required field to the `application` struct:
```go
GeminiAPIKey string `required:"true" arg:"gemini-api-key" env:"GEMINI_API_KEY" usage:"Gemini API key for LLM conflict resolution" display:"length"`
```

Update the gitClient creation in `Run`:
```go
conflictResolver := conflict.NewGeminiConflictResolver(a.GeminiAPIKey)
gitClient := gitclient.NewGitClient(a.GitURL, vaultLocalPath, a.GitBranch, conflictResolver)
```

Add import:
```go
"github.com/bborbe/agent/task/controller/pkg/conflict"
```

The `required:"true"` tag on `GeminiAPIKey` ensures the service refuses to start without a value — this is handled by the `service.Main` framework (same as `SentryDSN` in the same struct). No additional validation code needed.

### 6. Update `NewGitClient` call in `main_test.go` if it constructs a gitClient directly

Read `task/controller/main_test.go`. If it calls `gitclient.NewGitClient`, update the call to pass `nil` as the conflict resolver (tests don't need LLM resolution).

### 7. Add imports to `git_client.go` if not already present

`os.ReadFile`, `os.WriteFile`, and `os.Environ` are used in the new methods. Add `"os"` to imports if missing. Also add `"strings"` (used by `containsConflictMarkers`) and `"path/filepath"` (used by `resolveConflicts`) if missing.

### 8. Changelog

After implementing, add to `## Unreleased` in `CHANGELOG.md`:
```
- feat: resolve rebase merge conflicts via Gemini LLM before retrying push
- feat: require GEMINI_API_KEY startup flag in task/controller
```
</requirements>

<constraints>
- CQRS command format and task schema must not change (see `docs/kafka-schema-design.md`)
- Controller actor model must not change (see `docs/controller-design.md`)
- Gemini API key is a required startup parameter — `required:"true"` in struct tag; missing key prevents startup
- Fast path (push succeeds, no conflict): zero calls to Gemini API — the API is only called when `conflicted` files are detected after a rebase
- The `ConflictResolver` interface is defined in `pkg/gitclient` (not in `pkg/conflict`) to avoid import cycles — `gitclient` does not import `conflict`; `conflict` does not import `gitclient`; `main.go` imports both and wires them
- LLM prompt must clearly delimit file content as data (BEGIN/END markers) to prevent prompt injection
- If LLM returns content still containing conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`), treat it as a resolution failure — abort rebase, return error
- `abortRebase` must be called before returning any error from the conflict-resolution path
- Use `github.com/bborbe/errors` for all error wrapping — never `fmt.Errorf`, never `context.Background()` in pkg/ code
- Use `github.com/golang/glog` for logging — `glog.V(2).Infof` for resolution events
- File permissions `0600` for all `os.WriteFile` calls (gosec)
- Do NOT commit — dark-factory handles git
- All existing tests must pass
- `make precommit` passes in `task/controller`
</constraints>

<verification>
Verify `ConflictResolver` interface is defined in `gitclient` package:
```bash
grep -n "type ConflictResolver interface" task/controller/pkg/gitclient/git_client.go
```
Must show the interface definition.

Verify `conflictResolver` field in `gitClient` struct:
```bash
grep -n "conflictResolver" task/controller/pkg/gitclient/git_client.go
```
Must show the field and its usage in `resolveConflicts` and `pushWithRetry`.

Verify `GeminiAPIKey` is required in main.go:
```bash
grep -n "GeminiAPIKey\|gemini-api-key\|GEMINI_API_KEY" task/controller/main.go
```
Must show the field with `required:"true"`.

Verify Gemini resolver package exists:
```bash
ls task/controller/pkg/conflict/
```
Must show `gemini_resolver.go`, `gemini_resolver_test.go`, `conflict_suite_test.go`.

Verify conflict marker check function exists:
```bash
grep -n "containsConflictMarkers\|<<<<<<<" task/controller/pkg/gitclient/git_client.go
```
Must show the function and the marker string.

Verify `google.golang.org/genai` is a direct dependency:
```bash
grep "google.golang.org/genai" task/controller/go.mod
```
Must NOT show `// indirect` (direct dep after `go get`).

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
