// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	"github.com/bborbe/errors"
	"github.com/bborbe/validation"
)

// PromptInstruction is the text instruction sent to an agent job.
type PromptInstruction string

func (p PromptInstruction) String() string {
	return string(p)
}

func (p PromptInstruction) Validate(ctx context.Context) error {
	if p == "" {
		return errors.Wrapf(ctx, validation.Error, "instruction missing")
	}
	return nil
}
