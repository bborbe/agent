// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	"github.com/bborbe/errors"
	"github.com/bborbe/validation"
)

// TaskName is the human-readable name of a task.
type TaskName string

func (t TaskName) String() string {
	return string(t)
}

func (t TaskName) Validate(ctx context.Context) error {
	if t == "" {
		return errors.Wrapf(ctx, validation.Error, "name missing")
	}
	return nil
}
