// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/errors"
	libkv "github.com/bborbe/kv"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/controller/pkg/result"
)

// TaskResultCommandOperation is the CQRS command operation name for task result updates.
const TaskResultCommandOperation base.CommandOperation = "update-result"

// NewTaskResultExecutor creates a cdb.CommandObjectExecutorTx for update-result commands.
// Uses cdb.CommandObjectExecutorTxFunc adapter — same pattern as trading command handlers.
func NewTaskResultExecutor(writer result.ResultWriter) cdb.CommandObjectExecutorTx {
	return cdb.CommandObjectExecutorTxFunc(
		TaskResultCommandOperation,
		false, // sendResult: no result event needed
		func(ctx context.Context, tx libkv.Tx, commandObject cdb.CommandObject) (*base.EventID, base.Event, error) {
			var req lib.Task
			if err := commandObject.Command.Data.MarshalInto(ctx, &req); err != nil {
				return nil, nil, errors.Wrapf(
					ctx,
					cdb.ErrCommandObjectSkipped,
					"malformed Task command: %v",
					err,
				)
			}
			if err := req.Validate(ctx); err != nil {
				return nil, nil, errors.Wrapf(
					ctx,
					cdb.ErrCommandObjectSkipped,
					"invalid Task (taskID=%s): %v",
					req.TaskIdentifier,
					err,
				)
			}
			if err := writer.WriteResult(ctx, req); err != nil {
				return nil, nil, errors.Wrapf(
					ctx,
					err,
					"write result for task %s",
					req.TaskIdentifier,
				)
			}
			return nil, nil, nil
		},
	)
}
