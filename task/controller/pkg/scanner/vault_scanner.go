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
	"github.com/golang/glog"
	"github.com/google/uuid"
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

type fileEntry struct {
	hash           [32]byte
	taskIdentifier lib.TaskIdentifier
}

type vaultScanner struct {
	gitClient    gitclient.GitClient
	taskDir      string
	pollInterval time.Duration
	hashes       map[string]fileEntry
	trigger      <-chan struct{}
}

// NewVaultScanner creates a VaultScanner that polls git and scans the task directory.
func NewVaultScanner(
	gitClient gitclient.GitClient,
	taskDir string,
	pollInterval time.Duration,
	trigger <-chan struct{},
) VaultScanner {
	return &vaultScanner{
		gitClient:    gitClient,
		taskDir:      taskDir,
		pollInterval: pollInterval,
		hashes:       make(map[string]fileEntry),
		trigger:      trigger,
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
		case <-v.trigger:
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

	changed, deleted, written, writeError := v.scanFiles(ctx)

	if len(written) > 0 && !writeError {
		if err := v.gitClient.CommitAndPush(ctx, "[agent-task-controller] add task_identifier to tasks"); err != nil {
			glog.Warningf("git commit+push failed, skipping publish: %v", err)
			return
		}
	}

	result := ScanResult{Changed: changed, Deleted: deleted}
	select {
	case results <- result:
	default:
	}
}

func (v *vaultScanner) scanFiles(
	ctx context.Context,
) ([]lib.Task, []lib.TaskIdentifier, []string, bool) {
	taskDirPath := filepath.Join(v.gitClient.Path(), v.taskDir)
	fsys := os.DirFS(taskDirPath)
	seen := make(map[string]struct{})
	var changed []lib.Task
	var written []string
	writeError := false
	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		relPath := filepath.Join(v.taskDir, path)
		seen[relPath] = struct{}{}
		absPath := filepath.Join(taskDirPath, path)
		task, wrote, werr := v.processFile(ctx, fsys, path, absPath, relPath)
		if werr {
			writeError = true
		}
		if wrote != "" {
			written = append(written, wrote)
		}
		if task != nil {
			changed = append(changed, *task)
		}
		return nil
	}); err != nil {
		glog.Warningf("walk %s failed: %v", taskDirPath, err)
		return nil, nil, nil, false
	}
	return changed, v.collectDeleted(seen), written, writeError
}

// processFile handles a single .md file during a scan cycle.
// Returns (task, writtenRelPath, writeError).
func (v *vaultScanner) processFile(
	ctx context.Context,
	fsys fs.FS,
	path, absPath, relPath string,
) (*lib.Task, string, bool) {
	content, readErr := fs.ReadFile(fsys, path)
	if readErr != nil {
		glog.Warningf("failed to read %s: %v", relPath, readErr)
		return nil, "", false
	}
	hash := sha256.Sum256(content)
	if existing, ok := v.hashes[relPath]; ok && existing.hash == hash {
		return nil, "", false
	}
	fmYAML, err := extractFrontmatter(ctx, content)
	if err != nil {
		glog.Warningf("skipping %s: invalid frontmatter: %v", relPath, err)
		return nil, "", false
	}
	var fmMap map[string]interface{}
	if err := yaml.Unmarshal([]byte(fmYAML), &fmMap); err != nil {
		glog.Warningf("skipping %s: invalid frontmatter: %v", relPath, err)
		return nil, "", false
	}
	frontmatter := lib.TaskFrontmatter(fmMap)
	taskID, _ := fmMap["task_identifier"].(string)
	if taskID == "" {
		return v.injectAndStore(content, absPath, relPath)
	}
	v.hashes[relPath] = fileEntry{
		hash:           hash,
		taskIdentifier: lib.TaskIdentifier(taskID),
	}
	if frontmatter.Status() == "" {
		glog.Warningf("skipping %s: invalid frontmatter: status is empty", relPath)
		return nil, "", false
	}
	if frontmatter.Assignee() == "" {
		return nil, "", false
	}
	body := extractBody(content)
	return &lib.Task{
		TaskIdentifier: lib.TaskIdentifier(taskID),
		Frontmatter:    frontmatter,
		Content:        lib.TaskContent(body),
	}, "", false
}

// injectAndStore generates a UUID, writes it into the file, and records a sentinel hash entry.
// Returns (nil task, writtenRelPath, writeError).
func (v *vaultScanner) injectAndStore(
	content []byte,
	absPath, relPath string,
) (*lib.Task, string, bool) {
	id := uuid.New().String()
	newContent, injectErr := injectTaskIdentifier(content, id)
	if injectErr != nil {
		glog.Warningf("skipping %s: failed to inject task_identifier: %v", relPath, injectErr)
		return nil, "", false
	}
	if writeErr := os.WriteFile(absPath, newContent, 0600); writeErr != nil {
		glog.Warningf("failed to write %s: %v", relPath, writeErr)
		return nil, "", true
	}
	v.hashes[relPath] = fileEntry{hash: [32]byte{}, taskIdentifier: lib.TaskIdentifier(id)}
	return nil, relPath, false
}

func (v *vaultScanner) collectDeleted(seen map[string]struct{}) []lib.TaskIdentifier {
	var deleted []lib.TaskIdentifier
	for relPath, entry := range v.hashes {
		if _, ok := seen[relPath]; !ok {
			deleted = append(deleted, entry.taskIdentifier)
			delete(v.hashes, relPath)
		}
	}
	return deleted
}

func injectTaskIdentifier(content []byte, id string) ([]byte, error) {
	s := string(content)
	if strings.HasPrefix(s, "---\r\n") {
		return []byte("---\r\ntask_identifier: " + id + "\r\n" + s[5:]), nil
	}
	if strings.HasPrefix(s, "---\n") {
		return []byte("---\ntask_identifier: " + id + "\n" + s[4:]), nil
	}
	return nil, errors.Errorf(
		context.Background(),
		"content does not start with frontmatter delimiter",
	)
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

func extractBody(content []byte) string {
	s := string(content)
	const delim = "---"
	if !strings.HasPrefix(s, delim) {
		return s
	}
	rest := strings.TrimPrefix(s, delim)
	if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
	} else if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	}
	idx := strings.Index(rest, "\n---")
	if idx == -1 {
		return s
	}
	after := rest[idx+4:] // skip "\n---"
	if strings.HasPrefix(after, "\r\n") {
		after = after[2:]
	} else if strings.HasPrefix(after, "\n") {
		after = after[1:]
	}
	return after
}
