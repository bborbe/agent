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

// PromptIdentifierGenerator generates unique prompt identifiers.
type PromptIdentifierGenerator base.IdentifierGenerator[PromptIdentifier]

// PromptIdentifiers is a slice of PromptIdentifier.
type PromptIdentifiers []PromptIdentifier

// Contains returns true if the slice contains the given identifier.
func (p PromptIdentifiers) Contains(value PromptIdentifier) bool {
	return collection.Contains(p, value)
}

// PromptIdentifier uniquely identifies an agent prompt request.
type PromptIdentifier string

func (p PromptIdentifier) String() string {
	return string(p)
}

func (p PromptIdentifier) Bytes() []byte {
	return []byte(p)
}

func (p PromptIdentifier) Validate(ctx context.Context) error {
	if p == "" {
		return errors.Wrapf(ctx, validation.Error, "identifier missing")
	}
	return nil
}

func (p PromptIdentifier) Ptr() *PromptIdentifier {
	return &p
}

func (p PromptIdentifier) Equal(identifier base.ObjectIdentifier) bool {
	return p == identifier
}
