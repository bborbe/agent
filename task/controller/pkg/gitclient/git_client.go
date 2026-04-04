// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gitclient

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/bborbe/errors"
)

//counterfeiter:generate -o ../../mocks/git_client.go --fake-name FakeGitClient . GitClient
type GitClient interface {
	// EnsureCloned clones the repo if not present, validates if already cloned.
	EnsureCloned(ctx context.Context) error
	// Pull runs git pull on the local clone.
	Pull(ctx context.Context) error
	// CommitAndPush stages all changes, creates a commit with the given message, and pushes to the remote.
	CommitAndPush(ctx context.Context, message string) error
	// AtomicWriteAndCommitPush writes content to absPath and commits+pushes under a single lock.
	// No other git operation can interleave between the file write and the commit.
	AtomicWriteAndCommitPush(
		ctx context.Context,
		absPath string,
		content []byte,
		message string,
	) error
	// Path returns the local clone path.
	Path() string
}

type gitClient struct {
	gitURL    string
	localPath string
	branch    string
	mu        sync.Mutex
}

// NewGitClient creates a GitClient that uses the git binary via subprocess.
func NewGitClient(gitURL string, localPath string, branch string) GitClient {
	return &gitClient{
		gitURL:    gitURL,
		localPath: localPath,
		branch:    branch,
	}
}

func (g *gitClient) EnsureCloned(ctx context.Context) error {
	gitDir := filepath.Join(g.localPath, ".git")
	_, statErr := os.Stat(g.localPath)
	if os.IsNotExist(statErr) {
		// #nosec G204 -- binary is hardcoded "git", args are from trusted internal config
		cmd := exec.CommandContext(
			ctx,
			"git",
			"clone",
			"--branch",
			g.branch,
			"--single-branch",
			g.gitURL,
			g.localPath,
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			return errors.Wrapf(ctx, err, "git clone failed: %s", string(out))
		}
		return nil
	}
	_, gitDirErr := os.Stat(gitDir)
	if os.IsNotExist(gitDirErr) {
		return errors.Errorf(ctx, "directory %s exists but is not a git repository", g.localPath)
	}
	// #nosec G204 -- binary is hardcoded "git", localPath is from trusted internal config
	cmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "rev-parse", "--git-dir")
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.Wrapf(ctx, err, "invalid git repository at %s: %s", g.localPath, string(out))
	}
	return nil
}

func (g *gitClient) Pull(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pull(ctx)
}

func (g *gitClient) pull(ctx context.Context) error {
	// #nosec G204 -- binary is hardcoded "git", localPath is from trusted internal config
	cmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "pull", "--rebase")
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.Wrapf(ctx, err, "git pull failed: %s", string(out))
	}
	return nil
}

func (g *gitClient) CommitAndPush(ctx context.Context, message string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.commitAndPush(ctx, message)
}

func (g *gitClient) commitAndPush(ctx context.Context, message string) error {
	// #nosec G204 -- binary is hardcoded "git", localPath and message are from trusted internal config
	addCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "add", "-A")
	if out, err := addCmd.CombinedOutput(); err != nil {
		return errors.Wrapf(ctx, err, "git add failed: %s", string(out))
	}
	// #nosec G204 -- binary is hardcoded "git", localPath and message are from trusted internal config
	commitCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "commit", "-m", message)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return errors.Wrapf(ctx, err, "git commit failed: %s", string(out))
	}
	// #nosec G204 -- binary is hardcoded "git", localPath is from trusted internal config
	pushCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "push")
	if out, err := pushCmd.CombinedOutput(); err != nil {
		return errors.Wrapf(ctx, err, "git push failed: %s", string(out))
	}
	return nil
}

func (g *gitClient) AtomicWriteAndCommitPush(
	ctx context.Context,
	absPath string,
	content []byte,
	message string,
) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	// #nosec G306 -- 0600 is intentional for task files (gosec requirement)
	if err := os.WriteFile(absPath, content, 0600); err != nil {
		return errors.Wrapf(ctx, err, "write file %s", absPath)
	}
	return g.commitAndPush(ctx, message)
}

func (g *gitClient) Path() string {
	return g.localPath
}
