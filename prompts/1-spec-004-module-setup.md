---
status: created
spec: ["004"]
created: "2026-03-29T13:00:00Z"
branch: dark-factory/task-executor-service
---

<summary>
- task/executor gains its own go.mod, becoming a standalone Go module like prompt/controller
- All Kafka, K8s client-go, and standard dependencies are declared and resolved
- The lib module is wired via a local replace directive pointing to ../../lib
- tools.go declares build-tool imports so make generate/lint/test can run
- A Makefile and Dockerfile matching the prompt/controller skeleton are created
- A bare main.go exposes /healthz, /readiness, /metrics, /setloglevel with no Kafka wiring yet
- A main_test.go bootstraps the Ginkgo test runner and verifies the binary compiles
- make precommit passes with no business logic changes
</summary>

<objective>
Create `task/executor` as a standalone Go module (`github.com/bborbe/agent/task/executor`) with a bare HTTP server, matching the skeleton of `prompt/controller`. This is the prerequisite for the pipeline implementation in the next prompt.
</objective>

<context>
Read CLAUDE.md for project conventions, and all relevant `go-*.md` docs in `/home/node/.claude/docs/`.

Key files to read before making changes:
- `prompt/controller/go.mod` — reference go.mod to copy structure and direct dependency versions from
- `prompt/controller/tools.go` — reference tools.go to copy verbatim
- `prompt/controller/Makefile` — reference Makefile to copy (just change SERVICE name)
- `prompt/controller/Dockerfile` — reference Dockerfile to copy (task/executor uses scratch+alpine like prompt/controller, not git like task/controller)
- `prompt/executor/main.go` — bare HTTP server pattern to use as starting point for main.go
- `prompt/controller/main_test.go` — Ginkgo bootstrap pattern
</context>

<requirements>
### 1. Create `task/executor/` directory

The service lives at `task/executor/` in the repo root. Create the following files:

### 2. Create `task/executor/go.mod`

Module path: `github.com/bborbe/agent/task/executor`

Start from `prompt/controller/go.mod` as reference. Add K8s client-go as a direct dependency. The require block must include (copy versions directly from `prompt/controller/go.mod` for shared deps):

```
module github.com/bborbe/agent/task/executor

go 1.26.1

replace (
	github.com/opencontainers/runtime-spec => github.com/opencontainers/runtime-spec v1.2.0
)

replace (
	github.com/bborbe/agent/lib => ../../lib
)

require (
	github.com/IBM/sarama v1.47.0
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
	github.com/bborbe/vault-cli v0.50.0
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
	k8s.io/api v0.35.3
	k8s.io/apimachinery v0.35.3
	k8s.io/client-go v0.35.3
)
```

After writing this file, run `go mod tidy` inside `task/executor/` to resolve all transitive indirect dependencies and generate `go.sum`:

```bash
cd task/executor && go mod tidy
```

### 3. Create `task/executor/tools.go`

Copy verbatim from `prompt/controller/tools.go` (same license header, same `//go:build tools` tag, same import list):

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

### 4. Create `task/executor/Makefile`

Copy from `prompt/controller/Makefile` but change the SERVICE name:

```makefile
include ../../Makefile.variables
include ../../Makefile.precommit
include ../../Makefile.docker
include ../../Makefile.env
include ../../common.env

SERVICE = agent-task-executor
```

### 5. Create `task/executor/Dockerfile`

Copy verbatim from `prompt/controller/Dockerfile` — task/executor does not need git or openssh (no vault access), so use the scratch+alpine pattern (not the git-enabled task/controller Dockerfile):

```dockerfile
ARG DOCKER_REGISTRY=docker.quant.benjamin-borbe.de:443
FROM ${DOCKER_REGISTRY}/golang:1.26.1 AS build
ARG BUILD_GIT_COMMIT=none
ARG BUILD_DATE=unknown
COPY . /workspace
WORKDIR /workspace
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -mod=vendor -ldflags "-s" -a -installsuffix cgo -o /main
CMD ["/bin/bash"]

FROM ${DOCKER_REGISTRY}/alpine:3.23 AS alpine
RUN apk --no-cache add ca-certificates

FROM scratch
ARG BUILD_GIT_COMMIT=none
ARG BUILD_DATE=unknown
COPY --from=build /main /main
COPY --from=alpine /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /usr/local/go/lib/time/zoneinfo.zip /
ENV ZONEINFO=/zoneinfo.zip
ENV BUILD_GIT_COMMIT=${BUILD_GIT_COMMIT}
ENV BUILD_DATE=${BUILD_DATE}
ENTRYPOINT ["/main"]
```

### 6. Create `task/executor/main.go`

Bare HTTP server (no Kafka yet — that comes in the next prompt). Follows the `prompt/executor/main.go` pattern with the same endpoints as `prompt/controller/main.go`:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"os"
	"time"

	libhttp "github.com/bborbe/http"
	"github.com/bborbe/log"
	"github.com/bborbe/run"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN   string `required:"true"  arg:"sentry-dsn"   env:"SENTRY_DSN"   usage:"SentryDSN"            display:"length"`
	SentryProxy string `required:"false" arg:"sentry-proxy" env:"SENTRY_PROXY" usage:"Sentry Proxy"`
	Listen      string `required:"true"  arg:"listen"       env:"LISTEN"       usage:"address to listen to"`
}

func (a *application) Run(ctx context.Context, sentryClient libsentry.Client) error {
	glog.V(1).Infof("agent-task-executor started")

	return service.Run(
		ctx,
		a.createHTTPServer(),
	)
}

func (a *application) createHTTPServer() run.Func {
	return func(ctx context.Context) error {
		router := mux.NewRouter()
		router.Path("/healthz").Handler(libhttp.NewPrintHandler("OK"))
		router.Path("/readiness").Handler(libhttp.NewPrintHandler("OK"))
		router.Path("/metrics").Handler(promhttp.Handler())
		router.Path("/setloglevel/{level}").
			Handler(log.NewSetLoglevelHandler(ctx, log.NewLogLevelSetter(2, 5*time.Minute)))

		glog.V(2).Infof("starting http server listen on %s", a.Listen)
		return libhttp.NewServer(
			a.Listen,
			router,
		).Run(ctx)
	}
}
```

### 7. Create `task/executor/main_test.go`

Copy the pattern from `prompt/controller/main_test.go` (adjust module path):

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
		_, err = gexec.Build("github.com/bborbe/agent/task/executor", "-mod=mod")
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

### 8. Create `task/executor/mocks/mocks.go`

Placeholder file:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mocks
```

### 9. Run `go mod tidy`

```bash
cd task/executor && go mod tidy
```

This generates `go.sum` and populates indirect dependencies. Must exit 0.

### 10. Verify build

```bash
cd task/executor && go build ./...
```

Must exit 0.

### 11. Update `CHANGELOG.md`

Add or append to `## Unreleased` in the root `CHANGELOG.md`:

```
- feat: Add task/executor service skeleton with standalone go.mod, Makefile, Dockerfile, and bare HTTP server
```
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT implement Kafka consumer or K8s job spawning yet — this prompt is infrastructure only
- The module path must be exactly `github.com/bborbe/agent/task/executor`
- The replace directive for lib must use exactly `../../lib` as the local path
- All dependency versions must match prompt/controller's go.mod where the dep is shared
- `go mod tidy` must be run — do NOT hand-craft go.sum
- Do NOT add or remove any HTTP endpoints — exactly /healthz, /readiness, /metrics, /setloglevel/{level}
- The `agent-task-v1-event` Kafka topic and the lib.Task schema must not be changed
</constraints>

<verification>
```bash
cd task/executor && go mod tidy
```
Must exit 0.

```bash
cd task/executor && go build ./...
```
Must exit 0 (main.go must compile against new go.mod).

```bash
cd task/executor && make precommit
```
Must exit 0.
</verification>
