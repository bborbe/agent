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

	"github.com/bborbe/agent/task/controller/pkg/metrics"
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
	// AtomicReadModifyWriteAndCommitPush reads absPath, calls modify on its contents
	// to produce new contents, writes the result, and commits+pushes — all under
	// the gitclient mutex. modify must return the new file bytes or an error.
	// If modify returns an error, the file is not written and no commit is made.
	AtomicReadModifyWriteAndCommitPush(
		ctx context.Context,
		absPath string,
		modify func(current []byte) ([]byte, error),
		message string,
	) error
	// Path returns the local clone path.
	Path() string
}

// ConflictResolver resolves merge conflict markers in a single file's content.
// It receives the filename (for context) and the full file content including conflict markers.
// It returns the resolved content with all conflict markers removed, or an error.
type ConflictResolver interface {
	Resolve(ctx context.Context, filename string, content string) (string, error)
}

type gitClient struct {
	gitURL           string
	localPath        string
	branch           string
	mu               sync.Mutex
	conflictResolver ConflictResolver // nil means no LLM resolution available
}

// NewGitClient creates a GitClient that uses the git binary via subprocess.
// conflictResolver is called when a rebase produces merge conflicts; pass nil to disable LLM resolution.
func NewGitClient(
	gitURL string,
	localPath string,
	branch string,
	conflictResolver ConflictResolver,
) GitClient {
	return &gitClient{
		gitURL:           gitURL,
		localPath:        localPath,
		branch:           branch,
		conflictResolver: conflictResolver,
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
		metrics.GitPushTotal.WithLabelValues("success").Inc()
		return nil // fast path: push succeeded
	}
	glog.V(2).Infof("push failed (%v: %s), attempting fetch+rebase", pushErr, string(pushOut))

	// Fetch remote changes
	// #nosec G204 -- binary is hardcoded "git", args from trusted internal config
	fetchCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "fetch", "origin")
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		metrics.GitPushTotal.WithLabelValues("error").Inc()
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
		metrics.GitPushTotal.WithLabelValues("error").Inc()
		return errors.Wrapf(ctx, conflictErr, "check for conflicts after rebase")
	}
	if len(conflicted) > 0 {
		if err := g.handleConflictsAndPush(ctx, conflicted); err != nil {
			metrics.GitPushTotal.WithLabelValues("error").Inc()
			return err
		}
		metrics.GitPushTotal.WithLabelValues("conflict_resolved").Inc()
		return nil
	}
	if rebaseErr != nil {
		// Rebase failed but no conflict markers — some other error
		metrics.GitPushTotal.WithLabelValues("error").Inc()
		return errors.Wrapf(ctx, rebaseErr, "git rebase failed: %s", string(rebaseOut))
	}

	glog.V(2).Infof("rebase clean, retrying push")
	// #nosec G204 -- binary is hardcoded "git", args from trusted internal config
	retryCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "push")
	if out, err := retryCmd.CombinedOutput(); err != nil {
		metrics.GitPushTotal.WithLabelValues("error").Inc()
		return errors.Wrapf(ctx, err, "push retry failed: %s", string(out))
	}
	metrics.GitPushTotal.WithLabelValues("retry_success").Inc()
	return nil
}

// handleConflictsAndPush resolves conflicts in the given files, continues the rebase, and retries push.
func (g *gitClient) handleConflictsAndPush(ctx context.Context, conflicted []string) error {
	if g.conflictResolver == nil {
		g.abortRebase(ctx)
		return errors.Errorf(
			ctx,
			"rebase produced merge conflicts in %d file(s) and no conflict resolver is configured: %v",
			len(conflicted),
			conflicted,
		)
	}
	if err := g.resolveConflicts(ctx, conflicted); err != nil {
		g.abortRebase(ctx)
		return errors.Wrapf(ctx, err, "conflict resolution failed")
	}
	// After resolution, continue rebase
	// #nosec G204 -- binary is hardcoded "git", args from trusted internal config
	continueCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "rebase", "--continue")
	continueCmd.Env = append(os.Environ(), "GIT_EDITOR=true") // skip editor for commit message
	if out, err := continueCmd.CombinedOutput(); err != nil {
		g.abortRebase(ctx)
		return errors.Wrapf(ctx, err, "git rebase --continue failed: %s", string(out))
	}
	glog.V(2).Infof("conflict resolution complete, retrying push")
	// #nosec G204 -- binary is hardcoded "git", args from trusted internal config
	retryAfterResolve := exec.CommandContext(ctx, "git", "-C", g.localPath, "push")
	if out, err := retryAfterResolve.CombinedOutput(); err != nil {
		return errors.Wrapf(ctx, err, "push after conflict resolution failed: %s", string(out))
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

// resolveConflicts calls the ConflictResolver for each conflicted file, writes the resolved content,
// and stages the file with `git add`. Must be called with the rebase in-progress (inside the lock).
func (g *gitClient) resolveConflicts(ctx context.Context, conflicted []string) error {
	for _, relPath := range conflicted {
		absPath := filepath.Join(g.localPath, relPath)
		// #nosec G304 -- path constructed from trusted localPath + git-reported conflict list
		contentBytes, err := os.ReadFile(absPath)
		if err != nil {
			metrics.ConflictResolutionsTotal.WithLabelValues("error").Inc()
			return errors.Wrapf(ctx, err, "read conflicted file %s", relPath)
		}
		resolved, err := g.conflictResolver.Resolve(
			ctx,
			filepath.Base(relPath),
			string(contentBytes),
		)
		if err != nil {
			metrics.ConflictResolutionsTotal.WithLabelValues("error").Inc()
			return errors.Wrapf(ctx, err, "LLM resolution failed for %s", relPath)
		}
		// Safety check: resolved content must not contain conflict markers
		if containsConflictMarkers(resolved) {
			metrics.ConflictResolutionsTotal.WithLabelValues("error").Inc()
			return errors.Errorf(
				ctx,
				"LLM returned content still containing conflict markers for %s",
				relPath,
			)
		}
		// #nosec G306 G703 -- 0600 is intentional; absPath is trusted localPath + git-reported conflict list
		if err := os.WriteFile(absPath, []byte(resolved), 0600); err != nil {
			metrics.ConflictResolutionsTotal.WithLabelValues("error").Inc()
			return errors.Wrapf(ctx, err, "write resolved file %s", relPath)
		}
		// Stage the resolved file
		// #nosec G204 -- binary is hardcoded "git", args from trusted internal config
		addCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "add", relPath)
		if out, err := addCmd.CombinedOutput(); err != nil {
			metrics.ConflictResolutionsTotal.WithLabelValues("error").Inc()
			return errors.Wrapf(ctx, err, "git add resolved file %s: %s", relPath, string(out))
		}
		glog.V(2).Infof("resolved conflict in %s", relPath)
		metrics.ConflictResolutionsTotal.WithLabelValues("success").Inc()
	}
	return nil
}

// containsConflictMarkers returns true if the content contains git conflict markers.
// Checks for line-start anchored markers to avoid false positives from markdown content.
func containsConflictMarkers(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "<<<<<<<") ||
			strings.HasPrefix(line, "=======") ||
			strings.HasPrefix(line, ">>>>>>>") {
			return true
		}
	}
	return false
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

func (g *gitClient) AtomicReadModifyWriteAndCommitPush(
	ctx context.Context,
	absPath string,
	modify func(current []byte) ([]byte, error),
	message string,
) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	// #nosec G304 -- absPath is from trusted internal config (task file path)
	current, err := os.ReadFile(absPath)
	if err != nil {
		return errors.Wrapf(ctx, err, "read file for atomic modify")
	}
	updated, err := modify(current)
	if err != nil {
		return errors.Wrapf(ctx, err, "modify func failed")
	}
	// #nosec G306 -- 0600 is intentional for task files (gosec requirement)
	if err := os.WriteFile(absPath, updated, 0600); err != nil {
		return errors.Wrapf(ctx, err, "write file for atomic modify")
	}
	return g.commitAndPush(ctx, message)
}

func (g *gitClient) Path() string {
	return g.localPath
}
