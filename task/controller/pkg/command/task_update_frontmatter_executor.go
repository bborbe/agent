// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/errors"
	libkv "github.com/bborbe/kv"
	"github.com/golang/glog"

	lib "github.com/bborbe/agent/lib"
	delivery "github.com/bborbe/agent/lib/delivery"
	"github.com/bborbe/agent/task/controller/pkg/gitclient"
	"github.com/bborbe/agent/task/controller/pkg/metrics"
	"github.com/bborbe/agent/task/controller/pkg/result"
)

// UpdateFrontmatterCommandOperation is the CQRS command operation name for partial frontmatter update.
const UpdateFrontmatterCommandOperation base.CommandOperation = lib.UpdateFrontmatterCommandOperation

// NewUpdateFrontmatterExecutor creates a cdb.CommandObjectExecutorTx that atomically
// reads the task file, merges only the specified key-value pairs, and commits.
// All other frontmatter keys are left unchanged.
func NewUpdateFrontmatterExecutor(
	gitClient gitclient.GitClient,
	taskDir string,
) cdb.CommandObjectExecutorTx {
	return cdb.CommandObjectExecutorTxFunc(
		UpdateFrontmatterCommandOperation,
		true,
		func(ctx context.Context, tx libkv.Tx, commandObject cdb.CommandObject) (*base.EventID, base.Event, error) {
			var cmd lib.UpdateFrontmatterCommand
			if err := commandObject.Command.Data.MarshalInto(ctx, &cmd); err != nil {
				return nil, nil, errors.Wrapf(
					ctx,
					cdb.ErrCommandObjectSkipped,
					"malformed UpdateFrontmatterCommand: %v",
					err,
				)
			}
			// Empty updates with no body section is a no-op — nothing to write.
			if len(cmd.Updates) == 0 && cmd.Body == nil {
				return nil, nil, nil
			}
			taskDirPath := filepath.Join(gitClient.Path(), taskDir)
			absPath, _, err := result.FindTaskFilePath(ctx, taskDirPath, cmd.TaskIdentifier)
			if err != nil {
				metrics.FrontmatterCommandsTotal.WithLabelValues("update-frontmatter", "error").
					Inc()
				return nil, nil, errors.Wrapf(ctx, err, "find task file for update")
			}
			if absPath == "" {
				glog.Warningf(
					"update-frontmatter: task file not found for %s, skipping",
					cmd.TaskIdentifier,
				)
				metrics.FrontmatterCommandsTotal.WithLabelValues("update-frontmatter", "not_found").
					Inc()
				return nil, nil, nil
			}
			if err := gitClient.AtomicReadModifyWriteAndCommitPush(
				ctx,
				absPath,
				buildUpdateModifyFn(ctx, cmd.Updates, cmd.Body),
				fmt.Sprintf("[agent-task-controller] update frontmatter for task %s", cmd.TaskIdentifier),
			); err != nil {
				metrics.FrontmatterCommandsTotal.WithLabelValues("update-frontmatter", "error").
					Inc()
				return nil, nil, errors.Wrapf(
					ctx,
					err,
					"atomic update for task %s",
					cmd.TaskIdentifier,
				)
			}
			metrics.FrontmatterCommandsTotal.WithLabelValues("update-frontmatter", "success").Inc()
			return nil, nil, nil
		},
	)
}

func buildUpdateModifyFn(
	ctx context.Context,
	updates lib.TaskFrontmatter,
	bodySection *lib.BodySection,
) func([]byte) ([]byte, error) {
	return func(current []byte) ([]byte, error) {
		frontmatterStr, err := result.ExtractFrontmatter(ctx, current)
		if err != nil {
			return nil, errors.Wrapf(ctx, err, "extract frontmatter")
		}
		body, err := result.ExtractBody(ctx, current)
		if err != nil {
			return nil, errors.Wrapf(ctx, err, "extract body")
		}
		fm, err := parseTaskFrontmatter(frontmatterStr)
		if err != nil {
			return nil, errors.Wrapf(ctx, err, "parse frontmatter")
		}
		for k, v := range updates {
			fm[k] = v
		}
		if bodySection != nil {
			body = delivery.ReplaceOrAppendSection(body, bodySection.Heading, bodySection.Section)
		}
		return marshalFileContent(ctx, fm, body)
	}
}
