// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	"github.com/bborbe/collection"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	"github.com/bborbe/validation"
)

// TaskIdentifierGenerator generates unique task identifiers.
type TaskIdentifierGenerator base.IdentifierGenerator[TaskIdentifier]

// TaskIdentifiers is a slice of TaskIdentifier.
type TaskIdentifiers []TaskIdentifier

// Contains returns true if the slice contains the given identifier.
func (t TaskIdentifiers) Contains(value TaskIdentifier) bool {
	return collection.Contains(t, value)
}

// TaskIdentifier uniquely identifies an agent task.
type TaskIdentifier string

func (t TaskIdentifier) String() string {
	return string(t)
}

func (t TaskIdentifier) Bytes() []byte {
	return []byte(t)
}

func (t TaskIdentifier) Validate(ctx context.Context) error {
	if t == "" {
		return errors.Wrap(ctx, validation.Error, "identifier missing")
	}
	return nil
}

func (t TaskIdentifier) Ptr() *TaskIdentifier {
	return &t
}

func (t TaskIdentifier) Equal(identifier base.ObjectIdentifier) bool {
	return t == identifier
}
