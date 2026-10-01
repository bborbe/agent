// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/claude"
	"github.com/bborbe/agent/mocks"
)

// claudeServiceShim is a long-lived fake claude CLI: it stays alive across turns,
// records every turn it receives and answers each one with the same fixed result,
// so the contract rows that assert a response body hold for this backend too.
const claudeServiceShim = `trap 'echo "$$" > "$SHIM_DIR/exited.log"' EXIT
echo "$$" >> "$SHIM_DIR/starts.log"
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$SHIM_DIR/stdin.log"
  printf '{"type":"result","result":"session-result"}\n'
done
exit 0
`

// installClaudeShim writes the fake CLI into a fresh temp dir, puts that dir at the
// front of PATH and returns it so a backend can hand it to the session through
// ClaudeRunnerConfig.Env. The shim reads $SHIM_DIR because a process spawned by
// name never learns its own path.
func installClaudeShim() string {
	shimDir := GinkgoT().TempDir()
	shimPath := filepath.Join(shimDir, "claude")
	script := []byte("#!/bin/sh\n" + claudeServiceShim)
	Expect(os.WriteFile(shimPath, script, 0755)).To(Succeed()) //nolint:gosec
	originalPath := os.Getenv("PATH")
	DeferCleanup(func() {
		Expect(os.Setenv("PATH", originalPath)).To(Succeed())
	})
	Expect(os.Setenv("PATH", shimDir+":"+originalPath)).To(Succeed())
	return shimDir
}

// recordingFactory wraps a session factory and records what the service asked it
// for, so the shared contract rows can assert against a real backend exactly as
// they assert against the counterfeiter fake.
type recordingFactory struct {
	inner agentlib.SessionFactory

	mu      sync.Mutex
	created []string
	prompts []string
}

// Create returns the session for id, recording the id on the way through.
func (f *recordingFactory) Create(id string) agentlib.Session {
	f.mu.Lock()
	f.created = append(f.created, id)
	f.mu.Unlock()
	return &recordingSession{inner: f.inner.Create(id), owner: f}
}

// CreateCount reports how many sessions the factory has built.
func (f *recordingFactory) CreateCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.created)
}

// CreateArg reports the id the factory was asked for at index i.
func (f *recordingFactory) CreateArg(i int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.created[i]
}

// PromptArg reports the prompt the session received at index i.
func (f *recordingFactory) PromptArg(i int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prompts[i]
}

// recordingSession records the prompt before delegating to the claude session.
type recordingSession struct {
	inner agentlib.Session
	owner *recordingFactory
}

// Prompt records the turn and forwards it to the claude-backed session.
func (s *recordingSession) Prompt(ctx context.Context, prompt string) (string, error) {
	s.owner.mu.Lock()
	s.owner.prompts = append(s.owner.prompts, prompt)
	s.owner.mu.Unlock()
	return s.inner.Prompt(ctx, prompt)
}

// Close releases the underlying session.
func (s *recordingSession) Close(ctx context.Context) error {
	return s.inner.Close(ctx)
}

// newClaudeBackend wires the claude session implementation into the shared service
// in place of the pi one, with the CLI boundary faked by the shim.
func newClaudeBackend(shimDir string) *contractBackend {
	recorder := &recordingFactory{
		inner: claude.NewSessionFactory(
			claude.ClaudeRunnerConfig{Env: map[string]string{"SHIM_DIR": shimDir}},
			&mocks.ClaudePermissionDecider{},
		),
	}
	return &contractBackend{factory: recorder, observer: recorder}
}

var _ = Describe("Service with the claude session backend", func() {
	var shimDir string

	BeforeEach(func() {
		shimDir = installClaudeShim()
	})

	// The same rows as the mock-backed table, driven through the HTTP path with the
	// claude session implementation wired in. The table is not copied: both runs
	// share contractEntries, so a row added once is exercised by both backends.
	runContractTable(
		"frozen :9090 contract (claude backend)",
		func() *contractBackend { return newClaudeBackend(shimDir) },
	)

	It("holds one process across two prompts on one session id", func() {
		continuityShim := installClaudeShim()
		server := newTestServer(newClaudeBackend(continuityShim).factory, "")
		defer server.Close()

		status, err := postPrompt(server.URL, "sess-abc", "turn-one")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(200))

		status, err = postPrompt(server.URL, "sess-abc", "turn-two")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(200))

		// One process served both requests, and both payloads reached its stdin.
		starts := readShimLines(continuityShim, "starts.log")
		Expect(starts).To(HaveLen(1))
		stdin := readShimLines(continuityShim, "stdin.log")
		Expect(stdin).To(HaveLen(2))
		Expect(stdin[0]).To(ContainSubstring("turn-one"))
		Expect(stdin[1]).To(ContainSubstring("turn-two"))
	})
})

// readShimLines returns the non-empty lines a shim recorded in the named file.
func readShimLines(shimDir string, name string) []string {
	raw, err := os.ReadFile(filepath.Join(shimDir, name))
	Expect(err).NotTo(HaveOccurred())
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}
