// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package scanner

import (
	"context"
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bborbe/errors"
	"github.com/bborbe/vault-cli/pkg/domain"
	"github.com/golang/glog"
	"gopkg.in/yaml.v3"

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/controller/pkg/gitclient"
)

// ScanResult holds the outcome of a single vault scan cycle.
type ScanResult struct {
	Changed []lib.Task           // tasks whose content changed (new or modified)
	Deleted []lib.TaskIdentifier // task identifiers that were previously known but are now gone
}

//counterfeiter:generate -o ../../mocks/vault_scanner.go --fake-name FakeVaultScanner . VaultScanner
type VaultScanner interface {
	// Run starts the polling loop. Blocks until ctx is cancelled.
	// Results are sent to the provided channel; the caller owns the channel.
	Run(ctx context.Context, results chan<- ScanResult) error
}

type vaultScanner struct {
	gitClient    gitclient.GitClient
	taskDir      string
	pollInterval time.Duration
	hashes       map[string][32]byte
}

// NewVaultScanner creates a VaultScanner that polls git and scans the task directory.
func NewVaultScanner(
	gitClient gitclient.GitClient,
	taskDir string,
	pollInterval time.Duration,
) VaultScanner {
	return &vaultScanner{
		gitClient:    gitClient,
		taskDir:      taskDir,
		pollInterval: pollInterval,
		hashes:       make(map[string][32]byte),
	}
}

func (v *vaultScanner) Run(ctx context.Context, results chan<- ScanResult) error {
	ticker := time.NewTicker(v.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			v.runCycle(ctx, results)
		}
	}
}

func (v *vaultScanner) runCycle(ctx context.Context, results chan<- ScanResult) {
	if err := v.gitClient.Pull(ctx); err != nil {
		glog.Warningf("git pull failed: %v", err)
		return
	}
	glog.V(3).Infof("git pull succeeded, scanning %s", v.taskDir)
	changed, deleted := v.scanFiles(ctx)
	result := ScanResult{Changed: changed, Deleted: deleted}
	select {
	case results <- result:
	default:
	}
}

func (v *vaultScanner) scanFiles(ctx context.Context) ([]lib.Task, []lib.TaskIdentifier) {
	taskDirPath := filepath.Join(v.gitClient.Path(), v.taskDir)
	fsys := os.DirFS(taskDirPath)
	seen := make(map[string]struct{})
	var changed []lib.Task
	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		relPath := filepath.Join(v.taskDir, path)
		seen[relPath] = struct{}{}
		content, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			glog.Warningf("failed to read %s: %v", relPath, readErr)
			return nil
		}
		hash := sha256.Sum256(content)
		if existing, ok := v.hashes[relPath]; ok && existing == hash {
			return nil
		}
		v.hashes[relPath] = hash
		absPath := filepath.Join(taskDirPath, path)
		if task := v.parseTask(ctx, absPath, relPath); task != nil {
			changed = append(changed, *task)
		}
		return nil
	}); err != nil {
		glog.Warningf("walk %s failed: %v", taskDirPath, err)
		return nil, nil
	}
	return changed, v.collectDeleted(seen)
}

func (v *vaultScanner) collectDeleted(seen map[string]struct{}) []lib.TaskIdentifier {
	var deleted []lib.TaskIdentifier
	for relPath := range v.hashes {
		if _, ok := seen[relPath]; !ok {
			deleted = append(deleted, lib.TaskIdentifier(relPath))
			delete(v.hashes, relPath)
		}
	}
	return deleted
}

func (v *vaultScanner) parseTask(ctx context.Context, absPath, relPath string) *lib.Task {
	content, err := os.ReadFile(
		absPath,
	) // #nosec G304 -- absPath from trusted git clone + filepath.Walk
	if err != nil {
		glog.Warningf("skipping %s: invalid frontmatter: %v", relPath, err)
		return nil
	}
	frontmatter, err := extractFrontmatter(ctx, content)
	if err != nil {
		glog.Warningf("skipping %s: invalid frontmatter: %v", relPath, err)
		return nil
	}
	var domainTask domain.Task
	if err := yaml.Unmarshal([]byte(frontmatter), &domainTask); err != nil {
		glog.Warningf("skipping %s: invalid frontmatter: %v", relPath, err)
		return nil
	}
	if domainTask.Status == "" {
		glog.Warningf("skipping %s: invalid frontmatter: status is empty", relPath)
		return nil
	}
	if domainTask.Assignee == "" {
		return nil
	}
	name := strings.TrimSuffix(filepath.Base(absPath), ".md")
	return &lib.Task{
		TaskIdentifier: lib.TaskIdentifier(relPath),
		Name:           lib.TaskName(name),
		Status:         domainTask.Status,
		Phase:          domainTask.Phase,
		Assignee:       lib.TaskAssignee(domainTask.Assignee),
		Content:        lib.TaskContent(content),
	}
}

func extractFrontmatter(ctx context.Context, content []byte) (string, error) {
	s := string(content)
	const delim = "---"
	if !strings.HasPrefix(s, delim) {
		return "", errors.Errorf(ctx, "no frontmatter delimiter found")
	}
	rest := strings.TrimPrefix(s, delim)
	if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
	} else if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	}
	idx := strings.Index(rest, "\n---")
	if idx == -1 {
		return "", errors.Errorf(ctx, "frontmatter not closed")
	}
	return rest[:idx], nil
}
