// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	"github.com/bborbe/errors"
	"github.com/bborbe/validation"
)

// TaskAssignee identifies which agent type handles this task.
// Matched against AgentConfig CRD spec.assignee.
type TaskAssignee string

func (t TaskAssignee) String() string {
	return string(t)
}

func (t TaskAssignee) Validate(ctx context.Context) error {
	if t == "" {
		return errors.Wrapf(ctx, validation.Error, "assignee missing")
	}
	return nil
}
