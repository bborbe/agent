// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command

import (
	"context"
	"os"
	"path/filepath"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/errors"
	libkv "github.com/bborbe/kv"
	"github.com/bborbe/validation"
	"github.com/golang/glog"

	lib "github.com/bborbe/agent/lib"
	gitclient "github.com/bborbe/agent/task/controller/pkg/gitrestclient"
)

// NewCreateTaskExecutor creates a cdb.CommandObjectExecutorTx that materializes
// a new vault task file for the given task_identifier. If a file for that identifier
// already exists the command is a strict no-op (idempotent). Frontmatter must include
// "assignee" and "status"; missing fields return a wrapped validation error.
func NewCreateTaskExecutor(
	gitClient gitclient.GitClient,
	taskDir string,
) cdb.CommandObjectExecutorTx {
	return cdb.CommandObjectExecutorTxFunc(
		lib.CreateTaskCommandOperation,
		true,
		func(ctx context.Context, tx libkv.Tx, commandObject cdb.CommandObject) (*base.EventID, base.Event, error) {
			var cmd lib.CreateTaskCommand
			if err := commandObject.Command.Data.MarshalInto(ctx, &cmd); err != nil {
				return nil, nil, errors.Wrapf(
					ctx,
					cdb.ErrCommandObjectSkipped,
					"malformed CreateTaskCommand: %v",
					err,
				)
			}
			if err := cmd.TaskIdentifier.Validate(ctx); err != nil {
				return nil, nil, errors.Wrapf(ctx, err, "validate task_identifier")
			}
			if err := validateCreateTaskFrontmatter(ctx, cmd.Frontmatter); err != nil {
				return nil, nil, errors.Wrapf(ctx, err, "validate frontmatter")
			}
			taskDirPath := filepath.Join(gitClient.Path(), taskDir)
			absPath := filepath.Join(taskDirPath, string(cmd.TaskIdentifier)+".md")
			if _, err := os.Stat(absPath); err == nil {
				glog.Infof(
					"create-task: task file already exists for %s, skipping (idempotent)",
					cmd.TaskIdentifier,
				)
				return nil, nil, nil
			}
			content, err := buildCreateTaskContent(ctx, cmd)
			if err != nil {
				return nil, nil, errors.Wrapf(
					ctx,
					err,
					"build task file content for %s",
					cmd.TaskIdentifier,
				)
			}
			if err := gitClient.AtomicWriteAndCommitPush(
				ctx,
				absPath,
				content,
				"[agent-task-controller] create task "+string(cmd.TaskIdentifier),
			); err != nil {
				return nil, nil, errors.Wrapf(
					ctx,
					err,
					"atomic write and push for task %s",
					cmd.TaskIdentifier,
				)
			}
			glog.V(2).Infof("create-task: created task file for %s", cmd.TaskIdentifier)
			return nil, nil, nil
		},
	)
}

func validateCreateTaskFrontmatter(ctx context.Context, fm lib.TaskFrontmatter) error {
	if fm.Assignee() == "" {
		return errors.Wrap(ctx, validation.Error, "frontmatter missing required field: assignee")
	}
	if s, _ := fm.String("status"); s == "" {
		return errors.Wrap(ctx, validation.Error, "frontmatter missing required field: status")
	}
	return nil
}

func buildCreateTaskContent(ctx context.Context, cmd lib.CreateTaskCommand) ([]byte, error) {
	fm := make(lib.TaskFrontmatter)
	for k, v := range cmd.Frontmatter {
		fm[k] = v
	}
	fm["task_identifier"] = string(cmd.TaskIdentifier)
	return marshalFileContent(ctx, fm, cmd.Body)
}
