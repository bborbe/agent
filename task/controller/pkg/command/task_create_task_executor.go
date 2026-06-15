// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/errors"
	libkv "github.com/bborbe/kv"
	"github.com/bborbe/validation"
	"github.com/golang/glog"

	lib "github.com/bborbe/agent/lib"
	task "github.com/bborbe/agent/lib/command/task"
	gitclient "github.com/bborbe/agent/task/controller/pkg/gitrestclient"
	"github.com/bborbe/agent/task/controller/pkg/result"
	"github.com/bborbe/agent/task/controller/pkg/routing"
)

// NewCreateTaskExecutor creates a cdb.CommandObjectExecutorTx that materializes
// a new vault task file for the given task_identifier. If cmd.Title passes validation
// the file is written at tasks/{title}.md; otherwise it falls back to tasks/{task_identifier}.md.
// If a file for the resolved path already exists the command is a strict no-op (idempotent).
// Frontmatter must include "assignee" and "status"; missing fields return a wrapped validation error.
// Commands whose effective target vault (cmd.TargetVault or the legacy fallback) does not
// match vaultName are skipped without side effects (no git write, no error, no result event).
func NewCreateTaskExecutor(
	gitClient gitclient.GitClient,
	taskDir string,
	vaultName string,
) cdb.CommandObjectExecutorTx {
	return cdb.CommandObjectExecutorTxFunc(
		task.CreateCommandOperation,
		true,
		func(ctx context.Context, tx libkv.Tx, commandObject cdb.CommandObject) (*base.EventID, base.Event, error) {
			var cmd task.CreateCommand
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
			if !routing.ShouldProcess(cmd, vaultName) {
				effective := cmd.TargetVault
				if effective == "" {
					effective = routing.LegacyDefaultVault
				}
				glog.V(2).Infof(
					"create-task: skipped vault mismatch target=%q effective=%q vault=%q task=%s",
					cmd.TargetVault, effective, vaultName, cmd.TaskIdentifier,
				)
				return nil, nil, nil
			}
			if err := validateCreateTaskFrontmatter(ctx, cmd.Frontmatter); err != nil {
				return nil, nil, errors.Wrapf(ctx, err, "validate frontmatter")
			}
			taskDirPath := filepath.Join(gitClient.Path(), taskDir)
			absPath := resolveCreateTaskPath(ctx, taskDirPath, cmd)
			if _, err := os.Stat(absPath); err == nil {
				glog.Infof(
					"create-task: task file already exists at %s for %s, skipping (idempotent)",
					absPath, cmd.TaskIdentifier,
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
			glog.V(2).
				Infof("create-task: created task file at %s for %s", absPath, cmd.TaskIdentifier)
			return nil, nil, nil
		},
	)
}

// resolveCreateTaskPath returns the absolute path where the task file should be written.
// If cmd.Title passes validation and the title-derived path is unoccupied (or occupied by
// the same task), the title path is returned. Otherwise a WARN is logged and the UUID path
// is returned as fallback so the task is always materialized.
func resolveCreateTaskPath(
	ctx context.Context,
	taskDirPath string,
	cmd task.CreateCommand,
) string {
	uuidPath := filepath.Join(taskDirPath, string(cmd.TaskIdentifier)+".md")

	// Re-validate the command (defense-in-depth: sender may have been bypassed).
	if err := cmd.Validate(ctx); err != nil {
		glog.Warningf(
			"create-task: Title validation failed for task %s (%v); falling back to UUID path",
			cmd.TaskIdentifier, err,
		)
		return uuidPath
	}

	titlePath := filepath.Join(taskDirPath, cmd.Title+".md")

	// Reject titles containing path separators to prevent path traversal.
	if strings.ContainsAny(cmd.Title, "/\\") {
		glog.Warningf(
			"create-task: Title %q contains path separator; falling back to UUID path",
			cmd.Title,
		)
		return uuidPath
	}

	// Check if a file already exists at the title-derived path.
	existing, err := os.ReadFile(
		titlePath,
	) // #nosec G304 -- titlePath is guarded by strings.ContainsAny check above; defense-in-depth
	if err != nil {
		if os.IsNotExist(err) {
			// Title path is free — use it.
			return titlePath
		}
		// Unexpected read error: fall back to UUID path and log.
		glog.Warningf(
			"create-task: could not read %s (%v); falling back to UUID path for task %s",
			titlePath, err, cmd.TaskIdentifier,
		)
		return uuidPath
	}

	// File exists at title path — extract + parse frontmatter to check task_identifier.
	frontmatterStr, extractErr := result.ExtractFrontmatter(ctx, existing)
	if extractErr != nil {
		glog.Warningf(
			"create-task: could not extract frontmatter at %s (%v); treating as collision, falling back to UUID path for task %s",
			titlePath,
			extractErr,
			cmd.TaskIdentifier,
		)
		return uuidPath
	}
	fm, parseErr := parseTaskFrontmatter(frontmatterStr)
	if parseErr != nil {
		glog.Warningf(
			"create-task: could not parse frontmatter at %s (%v); treating as collision, falling back to UUID path for task %s",
			titlePath,
			parseErr,
			cmd.TaskIdentifier,
		)
		return uuidPath
	}
	existingID, _ := fm.String("task_identifier")
	if lib.TaskIdentifier(existingID) == cmd.TaskIdentifier {
		// Same task owns this file — idempotent.
		return titlePath
	}
	// Different task_identifier at the title path — collision.
	glog.Warningf(
		"create-task: title path %s is already occupied by task %q (current task: %s); falling back to UUID path",
		titlePath,
		existingID,
		cmd.TaskIdentifier,
	)
	return uuidPath
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

func buildCreateTaskContent(ctx context.Context, cmd task.CreateCommand) ([]byte, error) {
	fm := make(lib.TaskFrontmatter)
	for k, v := range cmd.Frontmatter {
		fm[k] = v
	}
	fm["task_identifier"] = string(cmd.TaskIdentifier)
	return marshalFileContent(ctx, fm, cmd.Body)
}
