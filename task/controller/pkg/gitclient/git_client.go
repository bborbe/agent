// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gitclient

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bborbe/errors"
	"github.com/golang/glog"
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
	// #nosec G204 -- binary is hardcoded "git", args from trusted internal config
	addCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "add", "-A")
	if out, err := addCmd.CombinedOutput(); err != nil {
		return errors.Wrapf(ctx, err, "git add failed: %s", string(out))
	}
	// #nosec G204 -- binary is hardcoded "git", args from trusted internal config
	commitCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "commit", "-m", message)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return errors.Wrapf(ctx, err, "git commit failed: %s", string(out))
	}
	if err := g.pushWithRetry(ctx); err != nil {
		return errors.Wrapf(ctx, err, "push failed")
	}
	return nil
}

// pushWithRetry attempts git push. On failure, fetches and rebases.
// If rebase is clean, retries push once. If conflicts are detected, aborts and returns an error.
func (g *gitClient) pushWithRetry(ctx context.Context) error {
	// #nosec G204 -- binary is hardcoded "git", args from trusted internal config
	pushCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "push")
	pushOut, pushErr := pushCmd.CombinedOutput()
	if pushErr == nil {
		return nil // fast path: push succeeded
	}
	glog.V(2).Infof("push failed (%v: %s), attempting fetch+rebase", pushErr, string(pushOut))

	// Fetch remote changes
	// #nosec G204 -- binary is hardcoded "git", args from trusted internal config
	fetchCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "fetch", "origin")
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		return errors.Wrapf(ctx, err, "git fetch failed: %s", string(out))
	}

	// Rebase onto remote
	// #nosec G204 -- binary is hardcoded "git", args from trusted internal config
	rebaseCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "rebase", "origin/"+g.branch)
	rebaseOut, rebaseErr := rebaseCmd.CombinedOutput()

	// Check for conflict markers regardless of rebase exit code
	conflicted, conflictErr := g.conflictedFiles(ctx)
	if conflictErr != nil {
		// Can't determine state — abort to be safe
		g.abortRebase(ctx)
		return errors.Wrapf(ctx, conflictErr, "check for conflicts after rebase")
	}
	if len(conflicted) > 0 {
		g.abortRebase(ctx)
		return errors.Errorf(
			ctx,
			"rebase produced merge conflicts in %d file(s): %v",
			len(conflicted),
			conflicted,
		)
	}
	if rebaseErr != nil {
		// Rebase failed but no conflict markers — some other error
		return errors.Wrapf(ctx, rebaseErr, "git rebase failed: %s", string(rebaseOut))
	}

	glog.V(2).Infof("rebase clean, retrying push")
	// #nosec G204 -- binary is hardcoded "git", args from trusted internal config
	retryCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "push")
	if out, err := retryCmd.CombinedOutput(); err != nil {
		return errors.Wrapf(ctx, err, "push retry failed: %s", string(out))
	}
	return nil
}

// conflictedFiles returns the list of files with unresolved conflict markers.
// Uses `git diff --name-only --diff-filter=U` which lists unmerged (conflicted) paths.
func (g *gitClient) conflictedFiles(ctx context.Context) ([]string, error) {
	// #nosec G204 -- binary is hardcoded "git", args from trusted internal config
	cmd := exec.CommandContext(
		ctx,
		"git",
		"-C",
		g.localPath,
		"diff",
		"--name-only",
		"--diff-filter=U",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(
			ctx,
			err,
			"git diff --name-only --diff-filter=U failed: %s",
			string(out),
		)
	}
	output := strings.TrimSpace(string(out))
	if output == "" {
		return nil, nil
	}
	return strings.Split(output, "\n"), nil
}

// abortRebase runs `git rebase --abort` to restore the working directory to a clean state.
// Errors are logged but not returned — this is a best-effort cleanup.
func (g *gitClient) abortRebase(ctx context.Context) {
	// #nosec G204 -- binary is hardcoded "git", args from trusted internal config
	cmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "rebase", "--abort")
	if out, err := cmd.CombinedOutput(); err != nil {
		glog.Warningf("git rebase --abort failed: %v: %s", err, string(out))
	}
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
