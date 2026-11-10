// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"context"

	"github.com/bborbe/errors"
)

// SessionID is the Claude CLI session id a spawned process must adopt.
//
// A non-empty value is passed to the CLI as --session-id. Empty means the CLI
// picks its own id, which is the behaviour when the field is unset; the argv is
// then unchanged. A non-empty value that is not a valid session-id shape — 1..128
// characters from [A-Za-z0-9-] — is rejected before the process starts.
//
// The package does not detect a collision with an existing transcript under the
// config directory: a supplied id is passed through and the CLI's own behaviour
// decides what to do with an id that already has a transcript (see
// findTranscriptFile in claude/transcript.go). The one collision the package
// itself causes is a rate-limit re-invoke: it re-runs buildCommand and starts a
// second process under the same --session-id while the abandoned attempt's
// transcript still exists.
type SessionID string

// validateSessionID rejects a non-empty SessionID that is not a valid session-id
// shape. The error names the value's length, never the value itself, because the
// id crosses the trust boundary into the CLI command line.
func validateSessionID(ctx context.Context, sessionID SessionID) error {
	if sessionID == "" || isValidSessionID(string(sessionID)) {
		return nil
	}
	return errors.Errorf(
		ctx,
		"invalid SessionID (length %d): must be 1..128 characters from [A-Za-z0-9-]",
		len(sessionID),
	)
}
