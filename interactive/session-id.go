// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

import (
	"net/http"
	"regexp"

	"github.com/bborbe/errors"
)

// sessionHeader is the request header a caller uses to address a session. Callers
// depend on the exact spelling, so it is a constant rather than a repeated literal.
const sessionHeader = "X-Session-Id"

// defaultSessionID is the conversation a request without the header is served from.
const defaultSessionID = "identity"

// sessionIDPattern is the allowed session id format: one to 64 characters, the first
// of which is not '-'. This is the security boundary, not a style preference — the id
// reaches a backend CLI as a command-line argument, so a leading '-' would be parsed
// as a flag, and '.' or '/' would escape a session directory if the id were used to
// build a storage path.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$`)

// sessionIDFromRequest resolves the conversation a prompt belongs to from the
// request's session header.
//
// An absent header is an existing caller and resolves to defaultSessionID, the one
// conversation this service served before the header existed, so nothing already
// talking to it is orphaned. A present header is the caller's own id and is accepted
// only if it matches sessionIDPattern — validated here, before any body is read and
// before any session exists.
//
// Absent and present-but-empty are deliberately different outcomes, and
// http.Header.Values is what tells them apart: Get collapses both to "". An empty
// value is a present header that does not match, so it is an error.
//
// The returned error deliberately does not carry the id: the id is caller-supplied,
// and a rejected id in a log line would let the log enumerate what callers asked for.
func sessionIDFromRequest(r *http.Request) (string, error) {
	values := r.Header.Values(sessionHeader)
	if len(values) == 0 {
		return defaultSessionID, nil
	}
	if !sessionIDPattern.MatchString(values[0]) {
		return "", errors.Errorf(r.Context(), "session id does not match the allowed format")
	}
	return values[0], nil
}
