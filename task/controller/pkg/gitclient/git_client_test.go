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
				"token",
				localPath,
				"main",
			)
		})

		It("returns the configured local path", func() {
			Expect(client.Path()).To(Equal(localPath))
		})
	})

	Describe("EnsureCloned", func() {
		Context("when localPath does not exist", func() {
			BeforeEach(func() {
				client = gitclient.NewGitClient(remoteDir, "", localPath, branch)
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
				client = gitclient.NewGitClient(remoteDir, "", localPath, branch)
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
				client = gitclient.NewGitClient(remoteDir, "", localPath, branch)
			})

			It("returns an error", func() {
				err := client.EnsureCloned(ctx)
				Expect(err).NotTo(BeNil())
			})
		})
	})

	Describe("Pull", func() {
		BeforeEach(func() {
			client = gitclient.NewGitClient(remoteDir, "", localPath, branch)
			err := client.EnsureCloned(ctx)
			Expect(err).To(BeNil())
		})

		It("succeeds on a valid repo", func() {
			err := client.Pull(ctx)
			Expect(err).To(BeNil())
		})
	})
})
