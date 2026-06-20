// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task

import (
	stderrors "errors"
)

// ErrTaskAlreadyExists is returned by the task controller's create-task
// executor when a task file already occupies the target filename in the vault.
// The controller writes nothing and the CQRS framework converts this error into
// a Failure on the result topic. Callers across repositories (e.g. the
// recurring-task-creator) match it via errors.Is to classify the collision as a
// benign, expected outcome of replaying a CreateCommand for an already-materialized
// task, rather than a genuine processing failure.
var ErrTaskAlreadyExists = stderrors.New("task file already exists at title path")
