---
status: completed
spec: [003-task-to-prompt-consumer]
summary: Created prompt/controller/go.mod, tools.go, and main_test.go establishing prompt/controller as a standalone Go module matching the task/controller pattern
container: agent-011-spec-003-module-setup
dark-factory-version: v0.69.0
created: "2026-03-28T00:00:00Z"
queued: "2026-03-28T11:50:08Z"
started: "2026-03-28T11:50:10Z"
completed: "2026-03-28T11:53:48Z"
branch: dark-factory/task-to-prompt-consumer
---

<summary>
- prompt/controller gains its own go.mod, becoming a standalone Go module like task/controller
- All Kafka and CQRS dependencies (bborbe/kafka, bborbe/cqrs) are declared and resolved
- The lib module is wired via a local replace directive pointing to ../../lib
- tools.go declares build-tool imports so make generate/lint/test can run
- A suite_test.go bootstraps the Ginkgo test runner for the package
- make precommit passes with no code changes to existing logic
</summary>

<objective>
Give `prompt/controller` its own `go.mod` (module: `github.com/bborbe/agent/prompt/controller`), matching the standalone-module pattern of `task/controller`. This is the prerequisite for the pipeline implementation in the next prompt.
</objective>

<context>
Read CLAUDE.md for project conventions.

Key files to read before making changes:
- `task/controller/go.mod` — reference go.mod to copy structure and versions from
- `task/controller/tools.go` — reference tools.go to copy verbatim
- `prompt/controller/main.go` — current main; imports must compile against the new go.mod
- `prompt/controller/mocks/mocks.go` — placeholder file, must remain compilable
</context>

<requirements>
### 1. Create `prompt/controller/go.mod`

Create the file with these exact contents (copy versions directly from `task/controller/go.mod`; omit `vault-cli` and `yaml.v3` since they are not needed by prompt/controller):

```
module github.com/bborbe/agent/prompt/controller

go 1.26.1

replace (
	github.com/opencontainers/runtime-spec => github.com/opencontainers/runtime-spec v1.2.0
)

replace (
	github.com/bborbe/agent/lib => ../../lib
)

require (
	github.com/bborbe/agent/lib v0.0.0-00010101000000-000000000000
	github.com/bborbe/cqrs v0.2.3
	github.com/bborbe/errors v1.5.8
	github.com/bborbe/http v1.26.7
	github.com/bborbe/kafka v1.22.9
	github.com/bborbe/log v1.6.8
	github.com/bborbe/run v1.9.12
	github.com/bborbe/sentry v1.9.13
	github.com/bborbe/service v1.9.7
	github.com/bborbe/time v1.25.6
	github.com/golang/glog v1.2.5
	github.com/golangci/golangci-lint/v2 v2.11.4
	github.com/google/addlicense v1.2.0
	github.com/google/osv-scanner/v2 v2.3.5
	github.com/google/uuid v1.6.0
	github.com/gorilla/mux v1.8.1
	github.com/incu6us/goimports-reviser/v3 v3.12.6
	github.com/kisielk/errcheck v1.10.0
	github.com/maxbrunsfeld/counterfeiter/v6 v6.12.1
	github.com/onsi/ginkgo/v2 v2.28.1
	github.com/onsi/gomega v1.39.1
	github.com/prometheus/client_golang v1.23.2
	github.com/securego/gosec/v2 v2.25.0
	github.com/segmentio/golines v0.13.0
	github.com/shoenig/go-modtool v0.7.1
	golang.org/x/vuln v1.1.4
)
```

After writing this file, run `go mod tidy` inside `prompt/controller/` to resolve all transitive indirect dependencies and generate `go.sum`:

```bash
cd prompt/controller && go mod tidy
```

### 2. Create `prompt/controller/tools.go`

Copy verbatim from `task/controller/tools.go` (same license header, same `//go:build tools` tag, same import list):

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build tools
// +build tools

package tools

import (
	_ "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
	_ "github.com/google/addlicense"
	_ "github.com/google/osv-scanner/v2/cmd/osv-scanner"
	_ "github.com/incu6us/goimports-reviser/v3"
	_ "github.com/kisielk/errcheck"
	_ "github.com/maxbrunsfeld/counterfeiter/v6"
	_ "github.com/onsi/ginkgo/v2/ginkgo"
	_ "github.com/securego/gosec/v2/cmd/gosec"
	_ "github.com/segmentio/golines"
	_ "github.com/shoenig/go-modtool"
	_ "golang.org/x/vuln/cmd/govulncheck"
)
```

### 3. Create `prompt/controller/main_test.go`

This bootstraps Ginkgo so `make test` works. Copy the pattern from `task/controller/main_test.go`:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/gexec"
)

var _ = Describe("Main", func() {
	It("Compiles", func() {
		var err error
		_, err = gexec.Build("github.com/bborbe/agent/prompt/controller", "-mod=mod")
		Expect(err).NotTo(HaveOccurred())
	})
})

//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate
func TestSuite(t *testing.T) {
	time.Local = time.UTC
	format.TruncatedDiff = false
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.Timeout = 60 * time.Second
	RunSpecs(t, "Test Suite", suiteConfig, reporterConfig)
}
```

### 4. Verify the mocks placeholder

Read `prompt/controller/mocks/mocks.go`. If it is a package declaration only (no imports), verify it declares `package mocks`. If it is empty or missing, create it:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mocks
```

### 5. Run `go mod tidy`

```bash
cd prompt/controller && go mod tidy
```

This generates `go.sum` and populates indirect dependencies. The command must exit 0.

### 6. Verify build

```bash
cd prompt/controller && go build ./...
```

Must exit 0. If any import in `main.go` is not resolvable, check that the dependency is in the `require` block and re-run `go mod tidy`.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT modify `main.go` logic — this prompt is infrastructure only
- Do NOT add or remove any existing HTTP endpoints
- The module path must be exactly `github.com/bborbe/agent/prompt/controller`
- The replace directive for lib must use exactly `../../lib` as the local path
- All dependency versions must match task/controller's go.mod where the dep is shared
- `go mod tidy` must be run — do NOT hand-craft go.sum
</constraints>

<verification>
```bash
cd prompt/controller && go mod tidy
```
Must exit 0.

```bash
cd prompt/controller && go build ./...
```
Must exit 0 (existing main.go must compile against new go.mod).

```bash
cd prompt/controller && make precommit
```
Must exit 0.
</verification>
