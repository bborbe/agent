// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	"github.com/bborbe/collection"
	"github.com/bborbe/errors"
	"github.com/bborbe/validation"
)

const (
	DonePromptStatus       PromptStatus = "done"
	RunningPromptStatus    PromptStatus = "running"
	NeedsInputPromptStatus PromptStatus = "needs_input"
	FailedPromptStatus     PromptStatus = "failed"
)

// AvailablePromptStatuses lists all valid prompt result statuses.
var AvailablePromptStatuses = PromptStatuses{
	DonePromptStatus,
	RunningPromptStatus,
	NeedsInputPromptStatus,
	FailedPromptStatus,
}

// PromptStatuses is a slice of PromptStatus.
type PromptStatuses []PromptStatus

// Contains returns true if the slice contains the given status.
func (p PromptStatuses) Contains(status PromptStatus) bool {
	return collection.Contains(p, status)
}

// PromptStatus represents the result status of a prompt execution.
type PromptStatus string

func (p PromptStatus) Validate(ctx context.Context) error {
	if !AvailablePromptStatuses.Contains(p) {
		return errors.Wrapf(ctx, validation.Error, "unknown prompt status '%s'", p)
	}
	return nil
}

func (p PromptStatus) String() string {
	return string(p)
}
