// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/bborbe/agent/claude"
	"github.com/bborbe/agent/interactive"
	"github.com/bborbe/agent/mocks"
)

// permissionSourceShim is a long-lived fake claude CLI that raises one permission
// request per turn and blocks until the verdict arrives, so one held process can serve
// both routes' turns on one shared session id. Each turn gets its own request_id, so
// the two raises are distinguishable in the recorded stdin. It reads $SHIM_DIR rather
// than dirname "$0" because the process is spawned by name.
const permissionSourceShim = `trap 'echo "$$" > "$SHIM_DIR/exited.log"' EXIT
n=0
while IFS= read -r line; do
  n=$((n+1))
  printf '%s\n' "$line" >> "$SHIM_DIR/stdin.log"
  printf '{"type":"control_request","request_id":"req-%s","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}\n' "$n"
  read -r resp
  printf '%s\n' "$resp" >> "$SHIM_DIR/stdin.log"
  printf '{"type":"result","result":"reply-%s"}\n' "$n"
done
exit 0
`

// installPermissionSourceShim writes the fake CLI into a fresh temp dir, puts that dir
// at the front of PATH and returns it, so the session can hand it $SHIM_DIR through
// ClaudeRunnerConfig.Env. PATH is restored when the spec ends.
func installPermissionSourceShim() string {
	shimDir := GinkgoT().TempDir()
	shimPath := filepath.Join(shimDir, "claude")
	script := []byte("#!/bin/sh\n" + permissionSourceShim)
	Expect(os.WriteFile(shimPath, script, 0755)).To(Succeed()) //nolint:gosec
	originalPath := os.Getenv("PATH")
	DeferCleanup(func() {
		Expect(os.Setenv("PATH", originalPath)).To(Succeed())
	})
	Expect(os.Setenv("PATH", shimDir+":"+originalPath)).To(Succeed())
	return shimDir
}

var _ = Describe("Permission request source", func() {
	It("stamps the raising route onto the permission request it hands the decider", func() {
		shimDir := installPermissionSourceShim()
		decider := &mocks.ClaudePermissionDecider{}
		decider.DecidePermissionReturns(claude.PermissionDecision{Allow: true}, nil)
		// The real session, not a fake that reads the context itself: the value must
		// cross the route → session → decider boundary for the assertion to mean
		// anything.
		factory := claude.NewSessionFactory(
			claude.ClaudeRunnerConfig{Env: map[string]string{"SHIM_DIR": shimDir}},
			decider,
		)
		svc := interactive.NewServiceWithPermissions(
			factory,
			":0",
			"",
			prometheus.NewRegistry(),
			interactive.AuthDisabled,
			interactive.CardConfig{PublicURL: testPublicURL},
			interactive.NewPermissionRegistry(),
			interactive.DefaultSessionIdleTimeout,
			interactive.DefaultMaxSessions,
		)
		server := httptest.NewServer(svc.Handler())
		defer server.Close()

		// Both turns name the same session id, so both routes resolve the same cached
		// entry and the same held process. A per-session (sticky) source would make
		// the second turn read the first turn's route and fail this spec.
		status, _, err := postPromptFull(server.URL, "shared", "hi")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))

		status, _, err = postA2A(server.URL, "", a2aSendBody(1, "shared", "hi"))
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))

		Expect(decider.DecidePermissionCallCount()).To(Equal(2))
		_, first := decider.DecidePermissionArgsForCall(0)
		_, second := decider.DecidePermissionArgsForCall(1)

		GinkgoWriter.Printf("source prompt=%q a2a=%q\n", first.Source, second.Source)

		Expect(first.ToolName).To(Equal("Bash"))
		Expect(second.ToolName).To(Equal("Bash"))
		Expect(first.Source).To(Equal(claude.PromptPermissionSource))
		Expect(second.Source).To(Equal(claude.A2APermissionSource))
		Expect(first.Source).NotTo(BeEmpty())
		Expect(second.Source).NotTo(BeEmpty())
		Expect(first.Source).NotTo(Equal(second.Source))
	})

	It("yields the empty source for a session whose caller named no route", func() {
		Expect(claude.PermissionSourceFromContext(context.Background())).To(
			Equal(claude.PermissionSource("")),
		)
	})
})
