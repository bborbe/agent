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

	"github.com/bborbe/errors"
	"github.com/golang/glog"
	"gopkg.in/yaml.v3"

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/controller/pkg/gitclient"
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
) ResultWriter {
	return &resultWriter{
		gitClient: gitClient,
		taskDir:   taskDir,
	}
}

type resultWriter struct {
	gitClient gitclient.GitClient
	taskDir   string
}

func (r *resultWriter) WriteResult(ctx context.Context, req lib.Task) error {
	glog.V(2).Infof("WriteResult: starting for task %s", req.TaskIdentifier)
	taskDirPath := filepath.Join(r.gitClient.Path(), r.taskDir)
	glog.V(3).Infof("WriteResult: scanning taskDir=%s", taskDirPath)
	fsys := os.DirFS(taskDirPath)

	var matchedAbsPath string
	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		content, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			glog.V(3).Infof("WriteResult: skip %s (read error: %v)", path, readErr)
			return nil
		}
		frontmatter, fmErr := extractFrontmatter(ctx, content)
		if fmErr != nil {
			glog.V(3).Infof("WriteResult: skip %s (frontmatter error: %v)", path, fmErr)
			return nil
		}
		var fm struct {
			TaskIdentifier string `yaml:"task_identifier"`
		}
		if umErr := yaml.Unmarshal([]byte(frontmatter), &fm); umErr != nil {
			glog.V(3).Infof("WriteResult: skip %s (unmarshal error: %v)", path, umErr)
			return nil
		}
		glog.V(3).Infof("WriteResult: file %s has task_identifier=%s", path, fm.TaskIdentifier)
		if lib.TaskIdentifier(fm.TaskIdentifier) == req.TaskIdentifier {
			matchedAbsPath = filepath.Join(taskDirPath, path)
			glog.V(2).Infof("WriteResult: matched file %s for task %s", matchedAbsPath, req.TaskIdentifier)
		}
		return nil
	}); err != nil {
		return errors.Wrapf(ctx, err, "walk task dir failed")
	}

	if matchedAbsPath == "" {
		glog.Warningf("task file not found for identifier %s, skipping", req.TaskIdentifier)
		return nil
	}

	marshaledFrontmatter, err := yaml.Marshal(map[string]any(req.Frontmatter))
	if err != nil {
		return errors.Wrapf(ctx, err, "marshal frontmatter failed")
	}

	newContent := []byte(
		"---\n" + string(marshaledFrontmatter) + "---\n" + sanitizeContent(string(req.Content)),
	)
	glog.V(2).Infof("WriteResult: writing and pushing for task %s", req.TaskIdentifier)
	if err := r.gitClient.AtomicWriteAndCommitPush(
		ctx,
		matchedAbsPath,
		newContent,
		fmt.Sprintf("[agent-task-controller] write result for task %s", req.TaskIdentifier),
	); err != nil {
		return errors.Wrapf(ctx, err, "atomic write and push failed")
	}

	glog.V(2).Infof("WriteResult: completed successfully for task %s", req.TaskIdentifier)
	return nil
}

func sanitizeContent(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.TrimRight(line, " \t") == "---" {
			lines[i] = `\-\-\-`
		}
	}
	return strings.Join(lines, "\n")
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
	before, _, found := strings.Cut(rest, "\n---")
	if !found {
		return "", errors.Errorf(ctx, "frontmatter not closed")
	}
	return before, nil
}
