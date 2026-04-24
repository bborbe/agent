// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package result

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bborbe/errors"
	libtime "github.com/bborbe/time"
	"github.com/golang/glog"
	"gopkg.in/yaml.v3"

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/controller/pkg/gitclient"
	"github.com/bborbe/agent/task/controller/pkg/metrics"
)

//counterfeiter:generate -o ../../mocks/result_writer.go --fake-name FakeResultWriter . ResultWriter

// ResultWriter writes a Task back to the vault task file.
type ResultWriter interface {
	WriteResult(ctx context.Context, req lib.Task) error
}

// NewResultWriter creates a ResultWriter that locates task files in the vault
// and writes the result, committing via gitClient.
func NewResultWriter(
	gitClient gitclient.GitClient,
	taskDir string,
	currentDateTime libtime.CurrentDateTimeGetter,
) ResultWriter {
	return &resultWriter{
		gitClient:       gitClient,
		taskDir:         taskDir,
		currentDateTime: currentDateTime,
	}
}

type resultWriter struct {
	gitClient       gitclient.GitClient
	taskDir         string
	currentDateTime libtime.CurrentDateTimeGetter
}

// FindTaskFilePath walks taskDirPath and returns the absolute path of the .md file
// whose frontmatter has task_identifier == id, plus the parsed existing frontmatter.
// Returns ("", nil, nil) when no match is found (not an error).
func FindTaskFilePath(
	ctx context.Context,
	taskDirPath string,
	id lib.TaskIdentifier,
) (string, lib.TaskFrontmatter, error) {
	fsys := os.DirFS(taskDirPath)
	var matchedAbsPath string
	var existingFrontmatter lib.TaskFrontmatter
	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		content, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			glog.V(3).Infof("FindTaskFilePath: skip %s (read error: %v)", path, readErr)
			return nil
		}
		frontmatter, fmErr := ExtractFrontmatter(ctx, content)
		if fmErr != nil {
			glog.V(3).Infof("FindTaskFilePath: skip %s (frontmatter error: %v)", path, fmErr)
			return nil
		}
		var fm struct {
			TaskIdentifier string `yaml:"task_identifier"`
		}
		if umErr := yaml.Unmarshal([]byte(frontmatter), &fm); umErr != nil {
			glog.V(3).Infof("FindTaskFilePath: skip %s (unmarshal error: %v)", path, umErr)
			return nil
		}
		glog.V(3).Infof("FindTaskFilePath: file %s has task_identifier=%s", path, fm.TaskIdentifier)
		if lib.TaskIdentifier(fm.TaskIdentifier) == id {
			matchedAbsPath = filepath.Join(taskDirPath, path)
			glog.V(2).Infof("FindTaskFilePath: matched file %s for task %s", matchedAbsPath, id)
			var existingFm lib.TaskFrontmatter
			if umErr := yaml.Unmarshal([]byte(frontmatter), &existingFm); umErr != nil {
				glog.V(3).Infof("FindTaskFilePath: could not unmarshal existing frontmatter for %s: %v", path, umErr)
			} else {
				existingFrontmatter = existingFm
			}
		}
		return nil
	}); err != nil {
		return "", nil, errors.Wrapf(ctx, err, "walk task dir failed")
	}
	return matchedAbsPath, existingFrontmatter, nil
}

func (r *resultWriter) WriteResult(ctx context.Context, req lib.Task) error {
	glog.V(2).Infof("WriteResult: starting for task %s", req.TaskIdentifier)
	taskDirPath := filepath.Join(r.gitClient.Path(), r.taskDir)
	glog.V(3).Infof("WriteResult: scanning taskDir=%s", taskDirPath)

	matchedAbsPath, existingFrontmatter, err := FindTaskFilePath(
		ctx,
		taskDirPath,
		req.TaskIdentifier,
	)
	if err != nil {
		return errors.Wrapf(ctx, err, "find task file path failed")
	}

	if matchedAbsPath == "" {
		glog.Warningf("task file not found for identifier %s, skipping", req.TaskIdentifier)
		metrics.ResultsWrittenTotal.WithLabelValues("not_found").Inc()
		return nil
	}

	merged := mergeFrontmatter(existingFrontmatter, req.Frontmatter)
	body := r.applyRetryCounter(merged, string(req.Content))

	marshaledFrontmatter, err := yaml.Marshal(map[string]any(merged))
	if err != nil {
		return errors.Wrapf(ctx, err, "marshal frontmatter failed")
	}

	newContent := []byte(
		"---\n" + string(marshaledFrontmatter) + "---\n" + body,
	)
	glog.V(2).Infof("WriteResult: writing and pushing for task %s", req.TaskIdentifier)
	if err := r.gitClient.AtomicWriteAndCommitPush(
		ctx,
		matchedAbsPath,
		newContent,
		fmt.Sprintf("[agent-task-controller] write result for task %s", req.TaskIdentifier),
	); err != nil {
		metrics.ResultsWrittenTotal.WithLabelValues("error").Inc()
		return errors.Wrapf(ctx, err, "atomic write and push failed")
	}

	glog.V(2).Infof("WriteResult: completed successfully for task %s", req.TaskIdentifier)
	metrics.ResultsWrittenTotal.WithLabelValues("success").Inc()
	return nil
}

func (r *resultWriter) applyRetryCounter(merged lib.TaskFrontmatter, body string) string {
	if string(merged.Status()) == "completed" {
		return body
	}
	if merged.SpawnNotification() {
		delete(merged, "spawn_notification")
		return body
	}
	// retry_count is authoritative in the task file — the executor bumped it
	// at spawn time (spec 011). The writer only applies escalation.
	retryCount := merged.RetryCount()
	if retryCount >= merged.MaxRetries() {
		merged["phase"] = "human_review"
		body += r.escalationSection(retryCount, merged)
	}
	return body
}

func (r *resultWriter) escalationSection(retryCount int, merged lib.TaskFrontmatter) string {
	ts := r.currentDateTime.Now().UTC().Format(time.RFC3339)
	return fmt.Sprintf(
		"\n## Retry Escalation\n\n- **Timestamp:** %s\n- **Attempts:** %d\n- **Assignee:** %s\n- **Last error:** see agent output above\n",
		ts,
		retryCount,
		string(merged.Assignee()),
	)
}

// mergeFrontmatter returns a new frontmatter map with all keys from existing,
// overridden by all keys from incoming. Neither input map is modified.
func mergeFrontmatter(existing, incoming lib.TaskFrontmatter) lib.TaskFrontmatter {
	merged := make(lib.TaskFrontmatter, len(existing)+len(incoming))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range incoming {
		merged[k] = v
	}
	return merged
}

// ExtractFrontmatter returns the YAML frontmatter string between the opening and
// closing "---" delimiters. Returns an error if delimiters are missing.
func ExtractFrontmatter(ctx context.Context, content []byte) (string, error) {
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
	before, _, found := strings.Cut(rest, "\n---")
	if !found {
		return "", errors.Errorf(ctx, "frontmatter not closed")
	}
	return before, nil
}

// ExtractBody returns the file body — the bytes after the closing "---\n" delimiter.
// Returns an empty string if the body is empty; error if delimiters are missing.
func ExtractBody(ctx context.Context, content []byte) (string, error) {
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
	_, after, found := strings.Cut(rest, "\n---\n")
	if !found {
		return "", errors.Errorf(ctx, "frontmatter not closed")
	}
	return after, nil
}
