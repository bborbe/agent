// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	"github.com/bborbe/errors"
	"github.com/bborbe/validation"
)

// TaskContent is the markdown body of a task after the frontmatter closing delimiter.
type TaskContent string

func (t TaskContent) String() string {
	return string(t)
}

func (t TaskContent) Validate(ctx context.Context) error {
	if len(t) == 0 {
		return errors.Wrapf(ctx, validation.Error, "content missing")
	}
	return nil
}
