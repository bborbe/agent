// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"
	"strings"

	"github.com/bborbe/errors"
	"github.com/bborbe/validation"
)

// Validate enforces CreateTaskCommand schema rules before publishing or processing.
// Title must be cross-platform safe (see rules below). Body must be ≤500 KiB when non-empty.
func (cmd CreateTaskCommand) Validate(ctx context.Context) error {
	return validation.All{
		validation.Name("Title", validateCreateTaskTitle(cmd.Title)),
		validation.Name("Body", validateCreateTaskBody(cmd.Body)),
	}.Validate(ctx)
}

func validateCreateTaskTitle(title string) validation.HasValidation {
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
			return err
		}
		if err := validateTitleForbiddenChars(ctx, title); err != nil {
			return err
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

func validateCreateTaskBody(body string) validation.HasValidation {
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
