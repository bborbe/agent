// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package scanner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// testGitClient is a simple test double for gitclient.GitClient.
// We cannot use mocks.FakeGitClient here because mocks imports scanner (for ScanResult),
// which would create an import cycle with the internal test package.
type testGitClient struct {
	path          string
	pullErr       error
	commitPushErr error
}

func (t *testGitClient) EnsureCloned(_ context.Context) error { return nil }

func (t *testGitClient) Pull(_ context.Context) error { return t.pullErr }

func (t *testGitClient) Path() string { return t.path }

func (t *testGitClient) CommitAndPush(_ context.Context, _ string) error {
	return t.commitPushErr
}

func (t *testGitClient) AtomicWriteAndCommitPush(
	_ context.Context,
	_ string,
	_ []byte,
	_ string,
) error {
	return t.commitPushErr
}

func mustInitGitRepo(dir string) {
	cmds := [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
		{"git", "-C", dir, "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		// #nosec G204 -- test helper: commands are hardcoded test setup git invocations
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		Expect(err).To(BeNil(), "cmd %v failed: %s", args, string(out))
	}
}

var _ = Describe("VaultScanner", func() {
	var (
		ctx     context.Context
		s       *vaultScanner
		tmpDir  string
		taskDir string
		fakeGit *testGitClient
		results chan ScanResult
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "scanner-test-*")
		Expect(err).To(BeNil())
		taskDir = "24 Tasks"
		Expect(os.MkdirAll(filepath.Join(tmpDir, taskDir), 0750)).To(Succeed())
		mustInitGitRepo(tmpDir)

		fakeGit = &testGitClient{path: tmpDir}
		results = make(chan ScanResult, 1)

		s = &vaultScanner{
			gitClient:    fakeGit,
			taskDir:      taskDir,
			pollInterval: time.Second,
			hashes:       make(map[string]fileEntry),
			trigger:      make(chan struct{}),
		}
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tmpDir)).To(Succeed())
	})

	Describe("processFile edge cases", func() {
		It("processes file with duplicate task_identifier (last wins)", func() {
			content := "---\ntask_identifier: first-uuid\nstatus: todo\nassignee: claude\ntask_identifier: real-uuid\n---\n# Dup task"
			absPath := filepath.Join(tmpDir, taskDir, "dup-key.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			s.runCycle(ctx, results)
			var result ScanResult
			Expect(results).To(Receive(&result))
			Expect(result.Changed).To(HaveLen(1))
			Expect(string(result.Changed[0].TaskIdentifier)).To(Equal("real-uuid"))
		})

		It("passes through file with non-empty unknown status", func() {
			content := "---\ntask_identifier: bad-status-uuid\nstatus: definitely_invalid_status\nassignee: claude\n---\n"
			absPath := filepath.Join(tmpDir, taskDir, "bad-status.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			s.runCycle(ctx, results)
			var result ScanResult
			Expect(results).To(Receive(&result))
			Expect(result.Changed).To(HaveLen(1))
			Expect(
				string(result.Changed[0].Frontmatter.Status()),
			).To(Equal("definitely_invalid_status"))
		})

		It("handles CRLF line endings in full cycle", func() {
			content := "---\r\ntask_identifier: crlf-uuid\r\nstatus: todo\r\nassignee: claude\r\n---\r\n# Task"
			absPath := filepath.Join(tmpDir, taskDir, "crlf-cycle.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			s.runCycle(ctx, results)
			var result ScanResult
			Expect(results).To(Receive(&result))
			Expect(result.Changed).To(HaveLen(1))
			Expect(string(result.Changed[0].Frontmatter.Assignee())).To(Equal("claude"))
		})
	})

	Describe("deduplicateFrontmatter", func() {
		It("returns original YAML unchanged when no duplicates", func() {
			input := "task_identifier: abc-123\nstatus: todo\nassignee: claude\n"
			out, hasDup, err := deduplicateFrontmatter(ctx, input)
			Expect(err).To(BeNil())
			Expect(hasDup).To(BeFalse())
			Expect(out).To(Equal(input))
		})

		It("deduplicates a single repeated key, last value wins", func() {
			input := "task_identifier: first\nstatus: todo\ntask_identifier: second\n"
			out, hasDup, err := deduplicateFrontmatter(ctx, input)
			Expect(err).To(BeNil())
			Expect(hasDup).To(BeTrue())
			var result map[string]interface{}
			Expect(yaml.Unmarshal([]byte(out), &result)).To(Succeed())
			Expect(result["task_identifier"]).To(Equal("second"))
			Expect(result["status"]).To(Equal("todo"))
		})

		It("deduplicates multiple repeated keys, last value wins each", func() {
			input := "task_identifier: first\nstatus: todo\ntask_identifier: second\nstatus: in_progress\n"
			out, hasDup, err := deduplicateFrontmatter(ctx, input)
			Expect(err).To(BeNil())
			Expect(hasDup).To(BeTrue())
			var result map[string]interface{}
			Expect(yaml.Unmarshal([]byte(out), &result)).To(Succeed())
			Expect(result["task_identifier"]).To(Equal("second"))
			Expect(result["status"]).To(Equal("in_progress"))
		})
	})

	Describe("injectTaskIdentifier", func() {
		It("injects task_identifier with LF line endings", func() {
			input := []byte("---\nstatus: todo\n---\n")
			result, err := injectTaskIdentifier(input, "test-id")
			Expect(err).To(BeNil())
			Expect(string(result)).To(Equal("---\ntask_identifier: test-id\nstatus: todo\n---\n"))
		})

		It("injects task_identifier with CRLF line endings", func() {
			input := []byte("---\r\nstatus: todo\r\n---\r\n")
			result, err := injectTaskIdentifier(input, "test-id")
			Expect(err).To(BeNil())
			Expect(
				string(result),
			).To(Equal("---\r\ntask_identifier: test-id\r\nstatus: todo\r\n---\r\n"))
		})

		It("returns error when content does not start with frontmatter delimiter", func() {
			input := []byte("no frontmatter")
			result, err := injectTaskIdentifier(input, "test-id")
			Expect(err).NotTo(BeNil())
			Expect(result).To(BeNil())
		})
	})

	Describe("NewVaultScanner", func() {
		It("returns a non-nil VaultScanner", func() {
			vs := NewVaultScanner(fakeGit, taskDir, time.Hour, nil)
			Expect(vs).NotTo(BeNil())
		})
	})

	Describe("Run", func() {
		It("returns nil when context is cancelled", func() {
			vs := NewVaultScanner(fakeGit, taskDir, time.Hour, nil)
			runCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() {
				done <- vs.Run(runCtx, make(chan ScanResult, 1))
			}()
			cancel()
			Eventually(done, 200*time.Millisecond).Should(Receive(BeNil()))
		})

		It("runs cycle when trigger fires", func() {
			content := "---\ntask_identifier: 44444444-4444-4444-8444-444444444444\nstatus: todo\nassignee: claude\n---\n# Triggered task"
			absPath := filepath.Join(tmpDir, taskDir, "trigger-task.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			trigger := make(chan struct{}, 1)
			vs := NewVaultScanner(fakeGit, taskDir, time.Hour, trigger)
			scanResults := make(chan ScanResult, 1)
			runCtx, cancel := context.WithCancel(ctx)
			defer cancel()

			done := make(chan error, 1)
			go func() {
				done <- vs.Run(runCtx, scanResults)
			}()

			trigger <- struct{}{}

			var result ScanResult
			Eventually(scanResults, time.Second).Should(Receive(&result))
			Expect(result.Changed).To(HaveLen(1))
			Expect(
				string(result.Changed[0].TaskIdentifier),
			).To(Equal("44444444-4444-4444-8444-444444444444"))

			cancel()
			Eventually(done, 200*time.Millisecond).Should(Receive(BeNil()))
		})
	})

	Describe("runCycle", func() {
		It("new file appears in Changed", func() {
			content := "---\ntask_identifier: 11111111-1111-4111-8111-111111111111\nstatus: todo\nassignee: claude\n---\n# New task"
			absPath := filepath.Join(tmpDir, taskDir, "new-task.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			s.runCycle(ctx, results)

			var result ScanResult
			Expect(results).To(Receive(&result))
			Expect(result.Changed).To(HaveLen(1))
			Expect(string(result.Changed[0].Frontmatter.Assignee())).To(Equal("claude"))
		})

		It("unchanged file is not in Changed on second cycle", func() {
			content := "---\ntask_identifier: 11111111-1111-4111-8111-111111111112\nstatus: todo\nassignee: claude\n---\n# Stable task"
			absPath := filepath.Join(tmpDir, taskDir, "stable-task.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			s.runCycle(ctx, results)
			var first ScanResult
			Expect(results).To(Receive(&first))
			Expect(first.Changed).To(HaveLen(1))

			s.runCycle(ctx, results)
			var second ScanResult
			Expect(results).To(Receive(&second))
			Expect(second.Changed).To(BeEmpty())
		})

		It("modified file appears in Changed on next cycle", func() {
			content := "---\ntask_identifier: 22222222-2222-4222-8222-222222222222\nstatus: todo\nassignee: claude\n---\n# Original"
			absPath := filepath.Join(tmpDir, taskDir, "modified-task.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			s.runCycle(ctx, results)
			Expect(results).To(Receive())

			updated := "---\ntask_identifier: 22222222-2222-4222-8222-222222222222\nstatus: in_progress\nassignee: claude\n---\n# Updated"
			Expect(os.WriteFile(absPath, []byte(updated), 0600)).To(Succeed())

			s.runCycle(ctx, results)
			var result ScanResult
			Expect(results).To(Receive(&result))
			Expect(result.Changed).To(HaveLen(1))
			Expect(string(result.Changed[0].Frontmatter.Status())).To(Equal("in_progress"))
		})

		It("drops result when channel is already full", func() {
			// Pre-fill the results channel (capacity 1)
			results <- ScanResult{}

			content := "---\ntask_identifier: 11111111-1111-4111-8111-111111111113\nstatus: todo\nassignee: claude\n---\n# Task"
			absPath := filepath.Join(tmpDir, taskDir, "drop-task.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			// runCycle should not block even though channel is full
			s.runCycle(ctx, results)
			// drain the pre-filled result (not the one we just tried to send)
			Expect(results).To(Receive())
		})

		It("skips cycle when git pull fails", func() {
			fakeGit.pullErr = context.DeadlineExceeded

			content := "---\ntask_identifier: 11111111-1111-4111-8111-111111111114\nstatus: todo\nassignee: claude\n---\n# Task"
			absPath := filepath.Join(tmpDir, taskDir, "pull-fail.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			s.runCycle(ctx, results)
			Consistently(results, 50*time.Millisecond).ShouldNot(Receive())
		})

		It("deleted file appears in Deleted on next cycle", func() {
			content := "---\ntask_identifier: 33333333-3333-4333-8333-333333333333\nstatus: todo\nassignee: claude\n---\n# Task"
			absPath := filepath.Join(tmpDir, taskDir, "deleted-task.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			s.runCycle(ctx, results)
			Expect(results).To(Receive())

			Expect(os.Remove(absPath)).To(Succeed())

			s.runCycle(ctx, results)
			var result ScanResult
			Expect(results).To(Receive(&result))
			Expect(result.Deleted).To(HaveLen(1))
			Expect(string(result.Deleted[0])).To(Equal("33333333-3333-4333-8333-333333333333"))
		})

		It("UUID injected when task_identifier absent", func() {
			content := "---\nstatus: todo\nassignee: claude\n---\n# Task without UUID"
			absPath := filepath.Join(tmpDir, taskDir, "no-uuid-task.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			s.runCycle(ctx, results)

			// Not published this cycle (write-back happened)
			var result ScanResult
			Expect(results).To(Receive(&result))
			Expect(result.Changed).To(BeEmpty())

			// File on disk now contains task_identifier
			written, err := os.ReadFile(absPath) // #nosec G304 -- test-only path
			Expect(err).To(BeNil())
			Expect(string(written)).To(ContainSubstring("task_identifier:"))
		})

		It("task published on second cycle after injection", func() {
			content := "---\nstatus: todo\nassignee: claude\n---\n# Task without UUID"
			absPath := filepath.Join(tmpDir, taskDir, "no-uuid-task2.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			// First cycle: injects UUID, does not publish
			s.runCycle(ctx, results)
			var first ScanResult
			Expect(results).To(Receive(&first))
			Expect(first.Changed).To(BeEmpty())

			// Second cycle: publishes with UUID
			s.runCycle(ctx, results)
			var second ScanResult
			Expect(results).To(Receive(&second))
			Expect(second.Changed).To(HaveLen(1))
			Expect(
				string(second.Changed[0].TaskIdentifier),
			).To(MatchRegexp(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`))
		})

		It("CommitAndPush failure suppresses ScanResult", func() {
			fakeGit.commitPushErr = context.DeadlineExceeded

			content := "---\nstatus: todo\nassignee: claude\n---\n# Task without UUID"
			absPath := filepath.Join(tmpDir, taskDir, "no-uuid-task3.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			s.runCycle(ctx, results)
			Consistently(results, 50*time.Millisecond).ShouldNot(Receive())
		})
	})
})
