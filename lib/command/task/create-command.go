// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task

import (
	"context"
	"regexp"
	"strings"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	"github.com/bborbe/validation"

	lib "github.com/bborbe/agent/lib"
)

// CreateCommandOperation is the Kafka command operation for creating a new vault task.
// Wire string unchanged: "create-task".
const CreateCommandOperation base.CommandOperation = "create-task"

// CreateCommand is the payload for CreateCommandOperation.
// JSON tags are byte-identical to the former lib.CreateTaskCommand for wire compatibility.
type CreateCommand struct {
	TaskIdentifier lib.TaskIdentifier  `json:"taskIdentifier"`
	Title          string              `json:"title"`
	Frontmatter    lib.TaskFrontmatter `json:"frontmatter"`
	Body           string              `json:"body,omitempty"`
	// TargetVault is the slug of the Obsidian vault this task belongs in.
	// Empty value means "use the controller's legacy default (openclaw)".
	// Wire format uses omitempty so legacy producers that never set it stay byte-compatible.
	TargetVault string `json:"targetVault,omitempty"`
}

// Validate enforces CreateCommand schema rules before publishing or processing.
func (cmd CreateCommand) Validate(ctx context.Context) error {
	return validation.All{
		validation.Name("Title", validateCreateTitle(cmd.Title)),
		validation.Name("Body", validateCreateBody(cmd.Body)),
		validation.Name("TargetVault", validateCreateTargetVault(cmd.TargetVault)),
	}.Validate(ctx)
}

func validateCreateTitle(title string) validation.HasValidation {
	return validation.HasValidationFunc(func(ctx context.Context) error {
		runes := []rune(title)
		if len(runes) == 0 {
			return errors.Wrap(ctx, validation.Error, "title must not be empty")
		}
		if len(runes) > 200 {
			return errors.Wrapf(
				ctx,
				validation.Error,
				"title length %d exceeds maximum 200 characters",
				len(runes),
			)
		}
		if err := validateTitleEdges(ctx, title); err != nil {
			return errors.Wrap(ctx, err, "validate title edges")
		}
		if err := validateTitleForbiddenChars(ctx, title); err != nil {
			return errors.Wrap(ctx, err, "validate title forbidden chars")
		}
		return validateTitleWindowsReserved(ctx, title)
	})
}

func validateTitleEdges(ctx context.Context, title string) error {
	if title[0] == ' ' || title[0] == '.' {
		return errors.Wrap(ctx, validation.Error, "title must not start with a space or dot")
	}
	if title[len(title)-1] == ' ' || title[len(title)-1] == '.' {
		return errors.Wrap(ctx, validation.Error, "title must not end with a space or dot")
	}
	if strings.Contains(title, "..") {
		return errors.Wrap(ctx, validation.Error, "title must not contain '..' (path traversal)")
	}
	return nil
}

func validateTitleForbiddenChars(ctx context.Context, title string) error {
	for _, r := range title {
		if r < 0x20 || r == 0x7F {
			return errors.Wrapf(
				ctx,
				validation.Error,
				"title contains forbidden control character U+%04X",
				r,
			)
		}
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return errors.Wrapf(ctx, validation.Error, "title contains forbidden character %q", r)
		}
	}
	return nil
}

func validateTitleWindowsReserved(ctx context.Context, title string) error {
	base := title
	if idx := strings.LastIndex(title, "."); idx > 0 {
		base = title[:idx]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return errors.Wrapf(
			ctx,
			validation.Error,
			"title %q is a forbidden Windows reserved name",
			title,
		)
	}
	return nil
}

func validateCreateBody(body string) validation.HasValidation {
	return validation.HasValidationFunc(func(ctx context.Context) error {
		if len(body) > 500*1024 {
			return errors.Wrapf(
				ctx,
				validation.Error,
				"body length %d bytes exceeds maximum 500 KiB",
				len(body),
			)
		}
		return nil
	})
}

var targetVaultSlugRegexp = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func validateCreateTargetVault(targetVault string) validation.HasValidation {
	return validation.HasValidationFunc(func(ctx context.Context) error {
		if targetVault == "" {
			// Empty is valid: legacy producers and the controller's
			// "use default vault" semantics both rely on it.
			return nil
		}
		if !targetVaultSlugRegexp.MatchString(targetVault) {
			return errors.Wrapf(
				ctx,
				validation.Error,
				"targetVault %q must match ^[a-z][a-z0-9-]*$",
				targetVault,
			)
		}
		return nil
	})
}
