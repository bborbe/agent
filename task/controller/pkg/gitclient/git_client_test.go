// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gitclient_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/task/controller/pkg/gitclient"
)

func mustInitRemoteRepo(dir string) {
	cmds := [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
		{"git", "-C", dir, "commit", "--allow-empty", "-m", "initial"},
	}
	for _, args := range cmds {
		// #nosec G204 -- test helper: commands are hardcoded test setup git invocations
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		Expect(err).To(BeNil(), "cmd %v failed: %s", args, string(out))
	}
}

var _ = Describe("GitClient", func() {
	var (
		ctx       context.Context
		localPath string
		remoteDir string
		client    gitclient.GitClient
		branch    string
	)

	BeforeEach(func() {
		ctx = context.Background()
		branch = "master"

		var err error
		remoteDir, err = os.MkdirTemp("", "gitclient-remote-*")
		Expect(err).To(BeNil())
		mustInitRemoteRepo(remoteDir)

		localPath = filepath.Join(os.TempDir(), "gitclient-local-test")
		Expect(os.RemoveAll(localPath)).To(Succeed())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(remoteDir)).To(Succeed())
		Expect(os.RemoveAll(localPath)).To(Succeed())
	})

	Describe("Path", func() {
		BeforeEach(func() {
			client = gitclient.NewGitClient(
				"https://github.com/owner/repo.git",
				localPath,
				"main",
				nil,
			)
		})

		It("returns the configured local path", func() {
			Expect(client.Path()).To(Equal(localPath))
		})
	})

	Describe("EnsureCloned", func() {
		Context("when localPath does not exist", func() {
			BeforeEach(func() {
				client = gitclient.NewGitClient(remoteDir, localPath, branch, nil)
			})

			It("clones the repository", func() {
				err := client.EnsureCloned(ctx)
				Expect(err).To(BeNil())
				_, statErr := os.Stat(filepath.Join(localPath, ".git"))
				Expect(statErr).To(BeNil())
			})
		})

		Context("when localPath exists and is a valid git repo", func() {
			BeforeEach(func() {
				client = gitclient.NewGitClient(remoteDir, localPath, branch, nil)
				err := client.EnsureCloned(ctx)
				Expect(err).To(BeNil())
			})

			It("succeeds without error", func() {
				err := client.EnsureCloned(ctx)
				Expect(err).To(BeNil())
			})
		})

		Context("when localPath exists but has no .git directory", func() {
			BeforeEach(func() {
				err := os.MkdirAll(localPath, 0750)
				Expect(err).To(BeNil())
				client = gitclient.NewGitClient(remoteDir, localPath, branch, nil)
			})

			It("returns an error", func() {
				err := client.EnsureCloned(ctx)
				Expect(err).NotTo(BeNil())
			})
		})
	})

	Describe("Pull", func() {
		BeforeEach(func() {
			client = gitclient.NewGitClient(remoteDir, localPath, branch, nil)
			err := client.EnsureCloned(ctx)
			Expect(err).To(BeNil())
		})

		It("succeeds on a valid repo", func() {
			err := client.Pull(ctx)
			Expect(err).To(BeNil())
		})
	})

	Describe("CommitAndPush", func() {
		BeforeEach(func() {
			// allow pushing to the non-bare remote's current branch
			// #nosec G204 -- test helper: command is hardcoded test setup git invocation
			out, configErr := exec.Command("git", "-C", remoteDir, "config", "receive.denyCurrentBranch", "ignore").
				CombinedOutput()
			Expect(
				configErr,
			).To(BeNil(), "config receive.denyCurrentBranch failed: %s", string(out))

			client = gitclient.NewGitClient(remoteDir, localPath, branch, nil)
			err := client.EnsureCloned(ctx)
			Expect(err).To(BeNil())
			// configure identity so git commit works in the cloned repo
			for _, args := range [][]string{
				{"git", "-C", localPath, "config", "user.email", "test@test.com"},
				{"git", "-C", localPath, "config", "user.name", "Test"},
			} {
				// #nosec G204 -- test helper: commands are hardcoded test setup git invocations
				out, identErr := exec.Command(args[0], args[1:]...).CombinedOutput()
				Expect(identErr).To(BeNil(), "cmd %v failed: %s", args, string(out))
			}
		})

		It("stages, commits, and pushes a new file", func() {
			taskFile := filepath.Join(localPath, "task.md")
			Expect(os.WriteFile(taskFile, []byte("hello"), 0600)).To(Succeed())

			err := client.CommitAndPush(ctx, "[test] add task.md")
			Expect(err).To(BeNil())

			// #nosec G204 -- test helper: command is hardcoded test verification git invocation
			out, err := exec.Command("git", "-C", localPath, "log", "--oneline").CombinedOutput()
			Expect(err).To(BeNil())
			Expect(string(out)).To(ContainSubstring("[test] add task.md"))
		})

		It("returns an error for an invalid path", func() {
			badClient := gitclient.NewGitClient(remoteDir, "/nonexistent/path", branch, nil)
			err := badClient.CommitAndPush(ctx, "should fail")
			Expect(err).NotTo(BeNil())
		})

		Context("when push fails due to remote advancing", func() {
			var secondLocalPath string

			BeforeEach(func() {
				secondLocalPath = filepath.Join(os.TempDir(), "gitclient-local-second")
				Expect(os.RemoveAll(secondLocalPath)).To(Succeed())

				// Clone a second local copy and push a commit to advance the remote
				secondClient := gitclient.NewGitClient(remoteDir, secondLocalPath, branch, nil)
				err := secondClient.EnsureCloned(ctx)
				Expect(err).To(BeNil())
				for _, args := range [][]string{
					{"git", "-C", secondLocalPath, "config", "user.email", "test2@test.com"},
					{"git", "-C", secondLocalPath, "config", "user.name", "Test2"},
				} {
					// #nosec G204 -- test helper: commands are hardcoded test setup git invocations
					out, identErr := exec.Command(args[0], args[1:]...).CombinedOutput()
					Expect(identErr).To(BeNil(), "cmd %v failed: %s", args, string(out))
				}
			})

			AfterEach(func() {
				Expect(os.RemoveAll(secondLocalPath)).To(Succeed())
			})

			It("rebases and retries push on clean rebase", func() {
				// Advance remote: second clone writes a different file and pushes
				remoteFile := filepath.Join(secondLocalPath, "remote.md")
				Expect(os.WriteFile(remoteFile, []byte("remote change"), 0600)).To(Succeed())
				for _, args := range [][]string{
					{"git", "-C", secondLocalPath, "add", "-A"},
					{"git", "-C", secondLocalPath, "commit", "-m", "remote advance"},
					{"git", "-C", secondLocalPath, "push"},
				} {
					// #nosec G204 -- test helper: commands are hardcoded test setup git invocations
					out, cmdErr := exec.Command(args[0], args[1:]...).CombinedOutput()
					Expect(cmdErr).To(BeNil(), "cmd %v failed: %s", args, string(out))
				}

				// Now write a different file in the first local clone and commit+push
				localFile := filepath.Join(localPath, "local.md")
				Expect(os.WriteFile(localFile, []byte("local change"), 0600)).To(Succeed())

				err := client.CommitAndPush(ctx, "[test] local change")
				Expect(err).To(BeNil())

				// Verify both commits are on the remote
				// #nosec G204 -- test helper: command is hardcoded test verification git invocation
				out, err := exec.Command("git", "-C", remoteDir, "log", "--oneline").
					CombinedOutput()
				Expect(err).To(BeNil())
				Expect(string(out)).To(ContainSubstring("remote advance"))
				Expect(string(out)).To(ContainSubstring("[test] local change"))
			})

			It(
				"returns a conflict error and leaves working directory clean when rebase conflicts",
				func() {
					// Advance remote: second clone writes to the same file with conflicting content
					conflictFile := filepath.Join(secondLocalPath, "conflict.md")
					Expect(
						os.WriteFile(conflictFile, []byte("remote version\n"), 0600),
					).To(Succeed())
					for _, args := range [][]string{
						{"git", "-C", secondLocalPath, "add", "-A"},
						{"git", "-C", secondLocalPath, "commit", "-m", "remote conflict base"},
						{"git", "-C", secondLocalPath, "push"},
					} {
						// #nosec G204 -- test helper: commands are hardcoded test setup git invocations
						out, cmdErr := exec.Command(args[0], args[1:]...).CombinedOutput()
						Expect(cmdErr).To(BeNil(), "cmd %v failed: %s", args, string(out))
					}

					// Pull in the remote base to the first local so it has the file
					// #nosec G204 -- test helper: command is hardcoded test setup git invocation
					out, pullErr := exec.Command("git", "-C", localPath, "pull", "--rebase").
						CombinedOutput()
					Expect(pullErr).To(BeNil(), "pull failed: %s", string(out))

					// Both clones now modify the same file with conflicting content
					// Second clone advances remote with a new version
					Expect(
						os.WriteFile(conflictFile, []byte("remote line A\n"), 0600),
					).To(Succeed())
					for _, args := range [][]string{
						{"git", "-C", secondLocalPath, "add", "-A"},
						{"git", "-C", secondLocalPath, "commit", "-m", "remote line A"},
						{"git", "-C", secondLocalPath, "push"},
					} {
						// #nosec G204 -- test helper: commands are hardcoded test setup git invocations
						out, cmdErr := exec.Command(args[0], args[1:]...).CombinedOutput()
						Expect(cmdErr).To(BeNil(), "cmd %v failed: %s", args, string(out))
					}

					// First local also modifies same file — this will conflict on rebase
					localConflictFile := filepath.Join(localPath, "conflict.md")
					Expect(
						os.WriteFile(localConflictFile, []byte("local line B\n"), 0600),
					).To(Succeed())

					err := client.CommitAndPush(ctx, "[test] local conflict")
					Expect(err).NotTo(BeNil())
					Expect(err.Error()).To(ContainSubstring("merge conflicts"))

					// Working directory must be clean — no rebase in progress
					// #nosec G204 -- test helper: command is hardcoded test verification git invocation
					rebaseDir := filepath.Join(localPath, ".git", "rebase-merge")
					_, statErr := os.Stat(rebaseDir)
					Expect(
						os.IsNotExist(statErr),
					).To(BeTrue(), "rebase-merge dir should not exist after abort")
				},
			)
		})
	})
})
