---
status: completed
summary: Injected BUILD_GIT_VERSION from git describe into all three Dockerfiles, Makefile.docker, task/controller and task/executor argument structs and startup logs, migrated from local lib/build-info-metrics to github.com/bborbe/metrics@v0.5.0, added internal struct tag tests, and added hack/check-build-git-version.sh smoke script.
container: agent-076-add-build-git-version-buildarg
dark-factory-version: v0.132.0
created: "2026-04-24T09:30:00Z"
queued: "2026-04-24T12:43:51Z"
started: "2026-04-24T12:44:00Z"
completed: "2026-04-24T12:59:15Z"
---

<summary>
- Agent service startup logs surface a human-readable build version (e.g. `v0.52.7`) alongside the short commit SHA.
- Build pipeline injects `BUILD_GIT_VERSION` derived from `git describe --tags --always --dirty`, so dirty-worktree builds are visible as `-dirty` in logs and image labels.
- All three agent Dockerfiles (`task/controller`, `task/executor`, `agent/claude`) consume the new build arg and expose it as an env var.
- `task/controller` and `task/executor` Argument structs gain a `BuildGitVersion` field that propagates from env into the startup log alongside the existing commit/date.
- Unit tests assert the new field is parsed, defaults to `"dev"`, and is wired through precisely like `BuildGitCommit`.
- Mirrors the canonical go-skeleton hygiene — no behavior change, just observability.
</summary>

<objective>
Mirror the go-skeleton `BUILD_GIT_VERSION` hygiene pattern across all agent services. Inject `git describe --tags --always --dirty` at build time so operators reading startup logs or image labels can identify the release (e.g. `v0.52.7`, `v0.52.7-dirty`, `1a1c570`) without cross-referencing `git log`.
</objective>

<context>
Read `CLAUDE.md` at the repo root for agent project conventions.

Relevant coding plugin docs (read before editing):
- `~/.claude/plugins/marketplaces/coding/docs/go-cli-guide.md` — Argument struct conventions (tag order, alignment).
- `~/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo/Gomega patterns used in this repo.
- `~/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — use `github.com/bborbe/errors` if error wrapping is introduced (unlikely here).
- `~/.claude/plugins/marketplaces/coding/docs/go-precommit.md` — precommit flow.

Reference files — **read these to mirror the pattern; do NOT modify them**:
- `~/Documents/workspaces/go-skeleton/Makefile.docker` — shows `--build-arg BUILD_GIT_VERSION=$$(git describe --tags --always --dirty)` placement.
- `~/Documents/workspaces/go-skeleton/Dockerfile` — shows `ARG BUILD_GIT_VERSION=dev`, `LABEL org.opencontainers.image.version="${BUILD_GIT_VERSION}"`, and `ENV BUILD_GIT_VERSION=${BUILD_GIT_VERSION}` placement.
- `~/Documents/workspaces/go-skeleton/main.go` (around line 44) — shows the Argument struct field shape:
  ```go
  BuildGitVersion string            `required:"false" arg:"build-git-version" env:"BUILD_GIT_VERSION" usage:"Build Git version"                                      default:"dev"`
  BuildGitCommit  string            `required:"false" arg:"build-git-commit"  env:"BUILD_GIT_COMMIT"  usage:"Build Git commit hash"                                  default:"none"`
  BuildDate       *libtime.DateTime `required:"false" arg:"build-date"        env:"BUILD_DATE"        usage:"Build timestamp (RFC3339)"`
  ```
  Note the `BuildGitVersion` field comes **before** `BuildGitCommit`.

Files you WILL modify (verified via `find . -name Dockerfile -not -path './vendor/*' -not -path './prompts/*'`):
- `Makefile.docker` (repo root — shared by all services)
- `task/controller/Dockerfile`
- `task/controller/main.go`
- `task/controller/main_test.go`
- `task/executor/Dockerfile`
- `task/executor/main.go`
- `task/executor/main_test.go`
- `agent/claude/Dockerfile`

Note: `agent/claude/main.go` and `agent/claude/cmd/run-task/main.go` do **not** have a `BuildGitCommit`/`BuildDate` Argument struct — they are interactive CLI binaries rather than long-running services. For `agent/claude` only the Dockerfile is updated (to keep the image metadata consistent); no Go changes are required there.
</context>

<requirements>

### 1. Update the shared `Makefile.docker` at the repo root

File: `Makefile.docker`

The existing `build` target has these build-arg lines (current state):

```makefile
	--build-arg DOCKER_REGISTRY=$(DOCKER_REGISTRY) \
	--build-arg BRANCH=$(BRANCH) \
	--build-arg BUILD_GIT_COMMIT=$$(git rev-parse --short HEAD) \
	--build-arg BUILD_DATE=$$(date -u +%Y-%m-%dT%H:%M:%SZ) \
```

Insert a new `--build-arg BUILD_GIT_VERSION` line **immediately before** the existing `BUILD_GIT_COMMIT` line so the resulting block reads:

```makefile
	--build-arg DOCKER_REGISTRY=$(DOCKER_REGISTRY) \
	--build-arg BRANCH=$(BRANCH) \
	--build-arg BUILD_GIT_VERSION=$$(git describe --tags --always --dirty) \
	--build-arg BUILD_GIT_COMMIT=$$(git rev-parse --short HEAD) \
	--build-arg BUILD_DATE=$$(date -u +%Y-%m-%dT%H:%M:%SZ) \
```

- Keep the trailing `\` line continuations.
- Keep the leading tab (not spaces) — this is a Makefile.
- Do NOT modify `check-go-mod`, `upload`, `clean`, `apply`, or `buca` targets.
- Do NOT remove `--no-cache` or any other existing flag.

### 2. Update every Dockerfile — add `BUILD_GIT_VERSION` ARG + LABEL + ENV

Apply the same pattern to **each** of these three files:
- `task/controller/Dockerfile`
- `task/executor/Dockerfile`
- `agent/claude/Dockerfile`

For every `ARG BUILD_GIT_COMMIT=none` line currently in the file, insert a new line **immediately before it**:

```dockerfile
ARG BUILD_GIT_VERSION=dev
```

For every `ENV BUILD_GIT_COMMIT=${BUILD_GIT_COMMIT}` line currently in the file, insert a new line **immediately before it**:

```dockerfile
ENV BUILD_GIT_VERSION=${BUILD_GIT_VERSION}
```

Additionally, in the **final runtime stage** of each Dockerfile (the stage that contains the `ENTRYPOINT`), add an OCI image version label. Place it **after** the final-stage `ARG BUILD_GIT_VERSION=dev` you just added, and before `COPY --from=build /main /main`:

```dockerfile
LABEL org.opencontainers.image.version="${BUILD_GIT_VERSION}"
```

- Do NOT remove existing `ARG BUILD_GIT_COMMIT`, `ARG BUILD_DATE`, `ENV BUILD_GIT_COMMIT`, `ENV BUILD_DATE` lines — only add siblings.
- Do NOT change `FROM` lines, `COPY` lines, `ENTRYPOINT`, or any other part of the file.
All three Dockerfiles (`task/controller/Dockerfile`, `task/executor/Dockerfile`, `agent/claude/Dockerfile`) are **multi-stage** with **two** `ARG BUILD_GIT_COMMIT=none` blocks — one in the build stage (`FROM ... AS build`) and one in the final runtime stage. Apply the pattern uniformly:

- Add `ARG BUILD_GIT_VERSION=dev` **before** each of the two `ARG BUILD_GIT_COMMIT=none` lines (so every Dockerfile gains TWO new ARG lines).
- Add `ENV BUILD_GIT_VERSION=${BUILD_GIT_VERSION}` **before** the single `ENV BUILD_GIT_COMMIT=${BUILD_GIT_COMMIT}` line in the final runtime stage (one new ENV per Dockerfile).
- Add `LABEL org.opencontainers.image.version="${BUILD_GIT_VERSION}"` in the final runtime stage, after that stage's `ARG BUILD_GIT_VERSION=dev` and before any `COPY --from=build` / `ENTRYPOINT` lines.

Read each Dockerfile before editing to confirm the two-stage shape — if any of the three turn out NOT to be multi-stage, adapt the pattern (single ARG + single ENV + LABEL), but log that finding so the reviewer can confirm.

Verify after editing that every Dockerfile still builds logically (no orphan ARG/ENV references).

### 3. Extend the Argument struct in `task/controller/main.go`

File: `task/controller/main.go`

Locate the `application` struct (around line 44). The existing trailing fields are:

```go
	BuildGitCommit string            `required:"false" arg:"build-git-commit" env:"BUILD_GIT_COMMIT" usage:"Build Git commit hash"                                       default:"none"`
	BuildDate      *libtime.DateTime `required:"false" arg:"build-date"       env:"BUILD_DATE"       usage:"Build timestamp (RFC3339)"`
```

Insert a new field **immediately before** `BuildGitCommit`:

```go
	BuildGitVersion string            `required:"false" arg:"build-git-version" env:"BUILD_GIT_VERSION" usage:"Build Git version (git describe --tags --always --dirty)" default:"dev"`
```

Important formatting rules:
- Use a leading tab, not spaces (this is Go source).
- Match the existing struct-tag alignment style in that file. If gofmt realigns neighbouring tags after your insertion, accept that realignment — run `gofmt -w main.go` or rely on `make precommit` to normalize.
- Preserve the `*libtime.DateTime` type width — the tag column should align cleanly with `BuildGitCommit` and `BuildDate`.

### 4. Extend the startup log in `task/controller/main.go`

Locate the existing startup log line (around line 63):

```go
	glog.V(1).Infof("agent-task-controller started commit=%s", a.BuildGitCommit)
```

Replace the format string and args so it reads:

```go
	glog.V(1).Infof("agent-task-controller started version=%s commit=%s", a.BuildGitVersion, a.BuildGitCommit)
```

Also update the Prometheus build-info metric wiring. Locate the existing line:

```go
	agentlib.NewBuildInfoMetrics().SetBuildInfo(a.BuildDate)
```

Replace with a call to the shared `github.com/bborbe/metrics` v0.5.0 helper, which accepts version + commit + buildDate:

```go
	libmetrics.NewBuildInfoMetrics().SetBuildInfo(a.BuildGitVersion, a.BuildGitCommit, a.BuildDate)
```

Update imports: remove (or keep if still used for other symbols) the `agentlib "github.com/bborbe/agent/lib"` import, and add:

```go
	libmetrics "github.com/bborbe/metrics"
```

Add the dependency and refresh vendor:

```bash
go get github.com/bborbe/metrics@v0.5.0
go mod tidy
go mod vendor
```

(This repo builds with `-mod=vendor`, so the vendor refresh is mandatory — tests and `make precommit` will fail otherwise.)

The shared helper registers a `build_info{version, commit}` gauge (no service-specific prefix) against `prometheus.DefaultRegisterer` at package init. Service identification comes from the Prometheus `job` label set by the scrape config, not a metric label. The helper's `SetBuildInfo` is a no-op when `buildDate` is nil (local `go run` without build args).

### 5. Extend the Argument struct in `task/executor/main.go`

File: `task/executor/main.go`

Same treatment as requirement 3. Locate (around line 46-47):

```go
	BuildGitCommit string            `required:"false" arg:"build-git-commit" env:"BUILD_GIT_COMMIT" usage:"Build Git commit hash"                                     default:"none"`
	BuildDate      *libtime.DateTime `required:"false" arg:"build-date"       env:"BUILD_DATE"       usage:"Build timestamp (RFC3339)"`
```

Insert a new field **immediately before** `BuildGitCommit`:

```go
	BuildGitVersion string            `required:"false" arg:"build-git-version" env:"BUILD_GIT_VERSION" usage:"Build Git version (git describe --tags --always --dirty)" default:"dev"`
```

### 6. Extend the startup log in `task/executor/main.go`

Locate the existing startup log line (around line 52):

```go
	glog.V(1).Infof("agent-task-executor started commit=%s", a.BuildGitCommit)
```

Replace with:

```go
	glog.V(1).Infof("agent-task-executor started version=%s commit=%s", a.BuildGitVersion, a.BuildGitCommit)
```

Apply the same BuildInfoMetrics migration as requirement 4. Replace:

```go
	agentlib.NewBuildInfoMetrics().SetBuildInfo(a.BuildDate)
```

with:

```go
	libmetrics.NewBuildInfoMetrics().SetBuildInfo(a.BuildGitVersion, a.BuildGitCommit, a.BuildDate)
```

and update imports accordingly (`libmetrics "github.com/bborbe/metrics"`).

### 7. Remove the deprecated `agent/lib/build-info-metrics.go`

The local helper in `agent/lib/build-info-metrics.go` is superseded by the shared `github.com/bborbe/metrics` package (v0.5.0). Delete both files:

- `lib/build-info-metrics.go`
- `lib/build-info-metrics_test.go`

Also remove any counterfeiter fake generated for this interface:

```bash
grep -rln 'BuildInfoMetrics' lib/mocks 2>/dev/null
```

If a mock file for this interface exists in `lib/mocks/`, delete it.

After removal, verify nothing still references the deleted symbol:

```bash
grep -rn 'agentlib\.NewBuildInfoMetrics\|lib\.BuildInfoMetrics' . --include='*.go' | grep -v vendor
```

Must return zero matches. If any reference remains (other than the controller/executor main.go lines you just migrated), migrate those too.

`go mod tidy` will drop the now-unused local dependency wiring. Run:

```bash
go mod tidy
```

### 8. Add unit tests — `task/controller/main_test.go`

File: `task/controller/main_test.go`

The existing test only verifies compilation. Add Ginkgo specs that exercise the new `BuildGitVersion` field on the `application` struct.

Because `application` is declared in `package main` and `main_test.go` is currently `package main_test` (external test package), switch the relevant assertions to use `reflect` to inspect struct tags directly — which is what the existing test style of this repo does for similar cases. Specifically, add the following pattern inside `Describe("Main", func() { ... })`:

```go
	Describe("application struct tags", func() {
		It("declares BuildGitVersion with correct env, arg, and default tags", func() {
			// Re-build the same struct shape inline to assert field tags exist as expected
			// in the binary under test — a pragmatic assertion that a developer can't
			// silently drop the field.
			type probe struct {
				BuildGitVersion string `required:"false" arg:"build-git-version" env:"BUILD_GIT_VERSION" default:"dev"`
			}
			f, ok := reflect.TypeOf(probe{}).FieldByName("BuildGitVersion")
			Expect(ok).To(BeTrue())
			Expect(f.Tag.Get("env")).To(Equal("BUILD_GIT_VERSION"))
			Expect(f.Tag.Get("arg")).To(Equal("build-git-version"))
			Expect(f.Tag.Get("default")).To(Equal("dev"))
		})
	})
```

Add `"reflect"` to the test file imports.

Additionally, add a behavior test that exercises the actual `application` struct from the compiled binary by using environment-variable-driven parsing. Because the struct lives in `package main`, the cleanest option is to write the test as an **internal** test file. Create a NEW file alongside `main.go`:

File: `task/controller/main_internal_test.go`

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"reflect"
	"testing"
)

func TestApplicationBuildGitVersionFieldExists(t *testing.T) {
	typ := reflect.TypeOf(application{})
	f, ok := typ.FieldByName("BuildGitVersion")
	if !ok {
		t.Fatalf("application struct is missing BuildGitVersion field")
	}
	if f.Type.Kind() != reflect.String {
		t.Fatalf("BuildGitVersion must be string, got %s", f.Type.Kind())
	}
	if got, want := f.Tag.Get("env"), "BUILD_GIT_VERSION"; got != want {
		t.Errorf("BuildGitVersion env tag = %q, want %q", got, want)
	}
	if got, want := f.Tag.Get("arg"), "build-git-version"; got != want {
		t.Errorf("BuildGitVersion arg tag = %q, want %q", got, want)
	}
	if got, want := f.Tag.Get("default"), "dev"; got != want {
		t.Errorf("BuildGitVersion default tag = %q, want %q", got, want)
	}
}

func TestApplicationBuildGitVersionFieldOrder(t *testing.T) {
	typ := reflect.TypeOf(application{})
	versionIdx, commitIdx := -1, -1
	for i := 0; i < typ.NumField(); i++ {
		switch typ.Field(i).Name {
		case "BuildGitVersion":
			versionIdx = i
		case "BuildGitCommit":
			commitIdx = i
		}
	}
	if versionIdx < 0 || commitIdx < 0 {
		t.Fatalf("both BuildGitVersion (%d) and BuildGitCommit (%d) must exist", versionIdx, commitIdx)
	}
	if versionIdx >= commitIdx {
		t.Errorf("BuildGitVersion (idx %d) must appear before BuildGitCommit (idx %d)", versionIdx, commitIdx)
	}
}
```

Rationale: the internal test file uses plain `testing.T` (no Ginkgo bootstrap needed) so it stays small and independent of the existing suite, while the external `main_test.go` continues to own the compile-check.

### 9. Add matching unit tests — `task/executor/main_internal_test.go`

Create an analogous NEW file at `task/executor/main_internal_test.go` with identical contents to the controller's internal test file from requirement 8 (same two `Test*` functions, same assertions, `package main`). The `application` struct for `task/executor` has the same two fields (`BuildGitVersion` → `BuildGitCommit` ordering), so the same assertions apply.

### 10. Add a Makefile-level smoke assertion

Add a one-shot shell assertion that the repo's `Makefile.docker` contains the new build-arg line. Because the repo already uses `make precommit` per service (not at the root), the cleanest place is a NEW file:

File: `hack/check-build-git-version.sh`

```bash
#!/usr/bin/env bash
# Copyright (c) 2026 Benjamin Borbe All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.
#
# Verifies every Dockerfile and the shared Makefile.docker wires BUILD_GIT_VERSION.
# Run from repo root.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

fail=0

if ! grep -q 'BUILD_GIT_VERSION=\$\$(git describe --tags --always --dirty)' Makefile.docker; then
	echo "FAIL: Makefile.docker missing BUILD_GIT_VERSION build-arg"
	fail=1
fi

while IFS= read -r -d '' dockerfile; do
	if ! grep -q '^ARG BUILD_GIT_VERSION=dev' "$dockerfile"; then
		echo "FAIL: $dockerfile missing 'ARG BUILD_GIT_VERSION=dev'"
		fail=1
	fi
	if ! grep -q '^ENV BUILD_GIT_VERSION=\${BUILD_GIT_VERSION}' "$dockerfile"; then
		echo "FAIL: $dockerfile missing 'ENV BUILD_GIT_VERSION=\${BUILD_GIT_VERSION}'"
		fail=1
	fi
	if ! grep -q 'LABEL org.opencontainers.image.version="\${BUILD_GIT_VERSION}"' "$dockerfile"; then
		echo "FAIL: $dockerfile missing OCI image.version LABEL"
		fail=1
	fi
done < <(find . -name Dockerfile -not -path './vendor/*' -not -path './prompts/*' -print0)

if [ "$fail" -ne 0 ]; then
	exit 1
fi
echo "OK: BUILD_GIT_VERSION wired in Makefile.docker and every Dockerfile"
```

Make it executable:

```bash
chmod +x hack/check-build-git-version.sh
```

If `hack/` does not exist, create it. Check first:

```bash
ls hack 2>/dev/null || mkdir hack
```

(`task/executor/hack/` exists but is service-scoped — this new script is repo-scoped.)

### 11. Update `CHANGELOG.md` only if the repo already maintains an unreleased section

Check `CHANGELOG.md`:
- If the file exists and has an `## Unreleased` section, append under it:
  ```
  - feat: Inject BUILD_GIT_VERSION (from `git describe --tags --always --dirty`) into all service images and surface it in startup logs of task/controller and task/executor.
  ```
- If no CHANGELOG.md or no Unreleased section exists, skip this step. Do NOT create the file for this hygiene change.

</requirements>

<constraints>
- Apply to ALL three Dockerfiles: `task/controller/Dockerfile`, `task/executor/Dockerfile`, `agent/claude/Dockerfile`. Do not scope to one service.
- Do NOT change or remove the existing `BUILD_GIT_COMMIT` or `BUILD_DATE` build-args, ARGs, ENVs, or log lines — only add siblings.
- The new `BuildGitVersion` field must appear **before** `BuildGitCommit` in every Argument struct so startup logs read version → commit → date in natural order.
- The `agent/claude` binaries (`agent/claude/main.go` and `agent/claude/cmd/run-task/main.go`) do NOT get Go changes — they have no `BuildGitCommit`/`BuildDate` Argument fields today. Only the Dockerfile is touched.
- Use `github.com/bborbe/errors` for any error wrapping (unlikely needed for this hygiene change).
- Do NOT call `context.Background()` in non-main code (unchanged rule — just a reminder).
- Preserve tab indentation in Makefile.docker (not spaces).
- Run `gofmt -w` on any modified `.go` file if struct-tag alignment drifts after insertion.
- `make precommit` must pass in `task/controller/` and in `task/executor/`.
- Do NOT commit — dark-factory handles git.
- No changelog entry unless the repo already has an `## Unreleased` section (see requirement 11).
</constraints>

<verification>

Run from the repo root:

```bash
# Every Dockerfile has the new ARG
grep -l 'ARG BUILD_GIT_VERSION=dev' task/controller/Dockerfile task/executor/Dockerfile agent/claude/Dockerfile
# Expect: all three paths printed.

# Every Dockerfile has the new ENV
grep -l 'ENV BUILD_GIT_VERSION=${BUILD_GIT_VERSION}' task/controller/Dockerfile task/executor/Dockerfile agent/claude/Dockerfile
# Expect: all three paths printed.

# Every Dockerfile has the OCI LABEL
grep -l 'LABEL org.opencontainers.image.version="${BUILD_GIT_VERSION}"' task/controller/Dockerfile task/executor/Dockerfile agent/claude/Dockerfile
# Expect: all three paths printed.

# Makefile.docker wires the build-arg
grep -n 'BUILD_GIT_VERSION=$$(git describe --tags --always --dirty)' Makefile.docker
# Expect: one match.

# Argument struct has the field in both services
grep -n 'BuildGitVersion' task/controller/main.go task/executor/main.go
# Expect: one match per file (the struct field).

# Startup log includes version
grep -n 'started version=' task/controller/main.go task/executor/main.go
# Expect: one match per file.

# Repo-level smoke script passes
bash hack/check-build-git-version.sh
# Expect: "OK: BUILD_GIT_VERSION wired in ..."

# Controller tests pass
cd task/controller && go test -mod=vendor -run TestApplicationBuildGitVersion ./... && cd -
# Expect: PASS.

# Executor tests pass
cd task/executor && go test -mod=vendor -run TestApplicationBuildGitVersion ./... && cd -
# Expect: PASS.

# Full precommit per modified service
cd task/controller && make precommit && cd -
cd task/executor && make precommit && cd -
# Both must exit 0.
```

After this lands and a deploy completes, live-check on the next deploy (informational — not part of prompt verification):

```bash
kubectlquant -n dev logs agent-task-controller-0 | grep -E 'started version=|Argument:.*Build'
```

Expect a line like:
```
agent-task-controller started version=v0.52.7 commit=615f9cc
```

</verification>
