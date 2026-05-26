---
status: completed
summary: Added tools.go and consolidated .PHONY declaration to task/controller Makefile
container: agent-exec-181-review-task-controller-fix-build-tooling
dark-factory-version: v0.173.0
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T22:23:09Z"
started: "2026-05-26T05:55:28Z"
completed: "2026-05-26T05:58:52Z"
---

<summary>
- Creates `tools.go` declaring all build tool dependencies in a `//go:build tools` package
- Adds `.PHONY` declarations to 10 Makefile targets that lack them
- Adds `GINKGO_VERSION` to `tools.env`
</summary>

<objective>
The Makefile has 10 targets without `.PHONY` declarations — if a file named `lint`, `format`, or `test` exists in the directory, Make will skip the target and try to build that file. Additionally, `tools.go` is missing, meaning build tools (counterfeiter, ginkgo, etc.) are not tracked in the Go module. After this fix, all Makefile targets are properly declared as phony, and all build tools are declared in `tools.go`.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- `task/controller/Makefile` — all targets and current .PHONY declarations
- `task/controller/tools.env` — current tool versions
- `task/controller/go.mod` — to find exact module path
</context>

<requirements>

### Part A: Add GINKGO_VERSION to tools.env

1. **Add to `tools.env`:**
   ```
   GINKGO_VERSION ?= v2.28.3
   ```

### Part B: Create tools.go

2. **Create `task/controller/tools.go`:**

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   //go:build tools
   // +build tools

   package main

   import (
       _ "github.com/google/addlicense"
       _ "github.com/maxbrunsfeld/counterfeiter/v6"
       _ "github.com/onsi/ginkgo/v2/ginkgo"
       _ "github.com/segmentio/golines"
       _ "github.com/sivukhin/gorevizor"
       _ "golang.org/x/tools/cmd/goimports"
       _ "golang.org/x/vuln/cmd/govulncheck"
   )
   ```

   Note: Verify actual import paths by running `go list -m all` from `task/controller/` and checking which tools are already transitive dependencies. Remove any that are not found from the import list above.

   Run `cd task/controller && go mod tidy` after creating `tools.go`.

### Part C: Add .PHONY to Makefile

3. **In `Makefile`**, find the `.PHONY` declaration and add the missing 10 targets. The current `.PHONY` declaration (around line 13) should be:

   ```makefile
   .PHONY: precommit ensure format generate check lint test vet errcheck gosec addlicense vulncheck osv-scanner trivy
   ```

   The targets `precommit ensure format generate check lint vet errcheck gosec addlicense` are currently missing from `.PHONY`.

4. **Run precommit:**
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0.

</requirements>

<constraints>
- Only change `task/controller/Makefile`, `task/controller/tools.env`, and create `task/controller/tools.go`
- Do NOT commit — dark-factory handles git
- Follow project conventions for Makefile and tools.go
</constraints>

<verification>
cd task/controller && make precommit
</verification>
