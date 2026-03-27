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
)

// testGitClient is a simple test double for gitclient.GitClient.
// We cannot use mocks.FakeGitClient here because mocks imports scanner (for ScanResult),
// which would create an import cycle with the internal test package.
type testGitClient struct {
	path    string
	pullErr error
}

func (t *testGitClient) EnsureCloned(_ context.Context) error { return nil }

func (t *testGitClient) Pull(_ context.Context) error { return t.pullErr }

func (t *testGitClient) Path() string { return t.path }

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
			hashes:       make(map[string][32]byte),
			trigger:      make(chan struct{}),
		}
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tmpDir)).To(Succeed())
	})

	Describe("parseTask", func() {
		It("returns Task for valid frontmatter with assignee", func() {
			content := "---\nstatus: todo\nassignee: claude\n---\n# Task title"
			absPath := filepath.Join(tmpDir, taskDir, "my-task.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())
			relPath := filepath.Join(taskDir, "my-task.md")

			task := s.parseTask(ctx, absPath, relPath)
			Expect(task).NotTo(BeNil())
			Expect(string(task.TaskIdentifier)).To(Equal(relPath))
			Expect(string(task.Name)).To(Equal("my-task"))
			Expect(string(task.Assignee)).To(Equal("claude"))
			Expect(string(task.Status)).To(Equal("todo"))
			Expect(string(task.Content)).To(Equal(content))
		})

		It("returns nil for valid frontmatter with empty assignee", func() {
			content := "---\nstatus: todo\n---\n# Task"
			absPath := filepath.Join(tmpDir, taskDir, "no-assignee.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			task := s.parseTask(ctx, absPath, filepath.Join(taskDir, "no-assignee.md"))
			Expect(task).To(BeNil())
		})

		It("returns nil for missing frontmatter delimiters", func() {
			content := "# Just a title\nno frontmatter here"
			absPath := filepath.Join(tmpDir, taskDir, "no-fm.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			task := s.parseTask(ctx, absPath, filepath.Join(taskDir, "no-fm.md"))
			Expect(task).To(BeNil())
		})

		It("returns nil for malformed YAML", func() {
			content := "---\nstatus: definitely_invalid_status\nassignee: claude\n---\n"
			absPath := filepath.Join(tmpDir, taskDir, "bad-yaml.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			task := s.parseTask(ctx, absPath, filepath.Join(taskDir, "bad-yaml.md"))
			Expect(task).To(BeNil())
		})

		It("returns nil when file cannot be read", func() {
			task := s.parseTask(ctx, "/nonexistent/path.md", "nonexistent.md")
			Expect(task).To(BeNil())
		})

		It("handles windows-style line endings in frontmatter", func() {
			content := "---\r\nstatus: todo\r\nassignee: claude\r\n---\r\n# Task"
			absPath := filepath.Join(tmpDir, taskDir, "crlf-task.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			task := s.parseTask(ctx, absPath, filepath.Join(taskDir, "crlf-task.md"))
			Expect(task).NotTo(BeNil())
			Expect(string(task.Assignee)).To(Equal("claude"))
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
			content := "---\nstatus: todo\nassignee: claude\n---\n# Triggered task"
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
			Expect(string(result.Changed[0].Name)).To(Equal("trigger-task"))

			cancel()
			Eventually(done, 200*time.Millisecond).Should(Receive(BeNil()))
		})
	})

	Describe("runCycle", func() {
		It("new file appears in Changed", func() {
			content := "---\nstatus: todo\nassignee: claude\n---\n# New task"
			absPath := filepath.Join(tmpDir, taskDir, "new-task.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			s.runCycle(ctx, results)

			var result ScanResult
			Expect(results).To(Receive(&result))
			Expect(result.Changed).To(HaveLen(1))
			Expect(string(result.Changed[0].Assignee)).To(Equal("claude"))
		})

		It("unchanged file is not in Changed on second cycle", func() {
			content := "---\nstatus: todo\nassignee: claude\n---\n# Stable task"
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
			content := "---\nstatus: todo\nassignee: claude\n---\n# Original"
			absPath := filepath.Join(tmpDir, taskDir, "modified-task.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			s.runCycle(ctx, results)
			Expect(results).To(Receive())

			updated := "---\nstatus: in_progress\nassignee: claude\n---\n# Updated"
			Expect(os.WriteFile(absPath, []byte(updated), 0600)).To(Succeed())

			s.runCycle(ctx, results)
			var result ScanResult
			Expect(results).To(Receive(&result))
			Expect(result.Changed).To(HaveLen(1))
			Expect(string(result.Changed[0].Status)).To(Equal("in_progress"))
		})

		It("drops result when channel is already full", func() {
			// Pre-fill the results channel (capacity 1)
			results <- ScanResult{}

			content := "---\nstatus: todo\nassignee: claude\n---\n# Task"
			absPath := filepath.Join(tmpDir, taskDir, "drop-task.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			// runCycle should not block even though channel is full
			s.runCycle(ctx, results)
			// drain the pre-filled result (not the one we just tried to send)
			Expect(results).To(Receive())
		})

		It("skips cycle when git pull fails", func() {
			fakeGit.pullErr = context.DeadlineExceeded

			content := "---\nstatus: todo\nassignee: claude\n---\n# Task"
			absPath := filepath.Join(tmpDir, taskDir, "pull-fail.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			s.runCycle(ctx, results)
			Consistently(results, 50*time.Millisecond).ShouldNot(Receive())
		})

		It("deleted file appears in Deleted on next cycle", func() {
			content := "---\nstatus: todo\nassignee: claude\n---\n# Task"
			absPath := filepath.Join(tmpDir, taskDir, "deleted-task.md")
			Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

			s.runCycle(ctx, results)
			Expect(results).To(Receive())

			Expect(os.Remove(absPath)).To(Succeed())

			s.runCycle(ctx, results)
			var result ScanResult
			Expect(results).To(Receive(&result))
			Expect(result.Deleted).To(HaveLen(1))
			Expect(string(result.Deleted[0])).To(ContainSubstring("deleted-task.md"))
		})
	})
})
