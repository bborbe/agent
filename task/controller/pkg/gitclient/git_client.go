// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gitclient

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bborbe/errors"
)

//counterfeiter:generate -o ../../mocks/git_client.go --fake-name FakeGitClient . GitClient
type GitClient interface {
	// EnsureCloned clones the repo if not present, validates if already cloned.
	EnsureCloned(ctx context.Context) error
	// Pull runs git pull on the local clone.
	Pull(ctx context.Context) error
	// Path returns the local clone path.
	Path() string
}

type gitClient struct {
	authURL   string
	localPath string
	branch    string
}

// NewGitClient creates a GitClient that uses the git binary via subprocess.
func NewGitClient(gitURL string, gitToken string, localPath string, branch string) GitClient {
	authURL := buildAuthURL(gitURL, gitToken)
	return &gitClient{
		authURL:   authURL,
		localPath: localPath,
		branch:    branch,
	}
}

func buildAuthURL(gitURL string, gitToken string) string {
	if gitToken == "" {
		return gitURL
	}
	u, err := url.Parse(gitURL)
	if err != nil || u.Scheme == "" {
		return fmt.Sprintf("https://x-access-token:%s@%s", gitToken, gitURL)
	}
	u.User = url.UserPassword("x-access-token", gitToken)
	return u.String()
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
			g.authURL,
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
	// #nosec G204 -- binary is hardcoded "git", localPath is from trusted internal config
	cmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "pull")
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.Wrapf(ctx, err, "git pull failed: %s", string(out))
	}
	return nil
}

func (g *gitClient) Path() string {
	return g.localPath
}
