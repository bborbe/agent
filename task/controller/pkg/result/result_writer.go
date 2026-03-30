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
	taskDirPath := filepath.Join(r.gitClient.Path(), r.taskDir)
	fsys := os.DirFS(taskDirPath)

	var matchedAbsPath string
	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		content, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			return nil
		}
		frontmatter, fmErr := extractFrontmatter(ctx, content)
		if fmErr != nil {
			return nil
		}
		var fm struct {
			TaskIdentifier string `yaml:"task_identifier"`
		}
		if umErr := yaml.Unmarshal([]byte(frontmatter), &fm); umErr != nil {
			return nil
		}
		if lib.TaskIdentifier(fm.TaskIdentifier) == req.TaskIdentifier {
			matchedAbsPath = filepath.Join(taskDirPath, path)
		}
		return nil
	}); err != nil {
		return errors.Wrapf(ctx, err, "walk task dir failed")
	}

	if matchedAbsPath == "" {
		glog.Warningf("task file not found for identifier %s, skipping", req.TaskIdentifier)
		return nil
	}

	marshaledFrontmatter, err := yaml.Marshal(map[string]interface{}(req.Frontmatter))
	if err != nil {
		return errors.Wrapf(ctx, err, "marshal frontmatter failed")
	}

	newContent := []byte("---\n" + string(marshaledFrontmatter) + "---\n" + string(req.Content))
	if writeErr := os.WriteFile(matchedAbsPath, newContent, 0600); writeErr != nil {
		return errors.Wrapf(ctx, writeErr, "write file failed")
	}

	if commitErr := r.gitClient.CommitAndPush(ctx, fmt.Sprintf("[agent-task-controller] write result for task %s", req.TaskIdentifier)); commitErr != nil {
		return errors.Wrapf(ctx, commitErr, "commit and push failed")
	}

	return nil
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
