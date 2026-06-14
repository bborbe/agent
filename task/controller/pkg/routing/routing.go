// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package routing decides whether a task controller should process a
// given CreateCommand based on the command's target vault and the
// controller's configured MY_VAULT.
package routing

import (
	"context"
	"regexp"

	"github.com/bborbe/errors"
	"github.com/bborbe/validation"

	task "github.com/bborbe/agent/lib/command/task"
)

// LegacyDefaultVault is the vault a controller acts on when a command
// leaves its TargetVault empty. Hard-coded per spec 044; do not make configurable.
const LegacyDefaultVault = "openclaw"

// targetVaultSlugRegexp must stay in sync with the same regex on
// task.CreateCommand.Validate (lib/command/task/create-command.go).
var targetVaultSlugRegexp = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ValidateMyVault returns a wrapped validation error when myVault is empty
// or does not match the slug regex ^[a-z][a-z0-9-]*$.
func ValidateMyVault(ctx context.Context, myVault string) error {
	if myVault == "" {
		return errors.Wrap(ctx, validation.Error, "MY_VAULT must not be empty")
	}
	if !targetVaultSlugRegexp.MatchString(myVault) {
		return errors.Wrapf(
			ctx,
			validation.Error,
			"MY_VAULT %q must match ^[a-z][a-z0-9-]*$",
			myVault,
		)
	}
	return nil
}

// ShouldProcess returns true iff the controller's myVault owns this command.
// An empty cmd.TargetVault falls back to LegacyDefaultVault (spec 044 AC 12).
// A non-empty cmd.TargetVault is compared verbatim (no case-folding).
func ShouldProcess(cmd task.CreateCommand, myVault string) bool {
	effective := cmd.TargetVault
	if effective == "" {
		effective = LegacyDefaultVault
	}
	return effective == myVault
}
