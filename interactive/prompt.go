// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/golang/glog"
)

// maxPromptBytes bounds the body a single request may carry, on both POST /prompt and
// POST /a2a. An unbounded read would let one caller exhaust the pod's memory. Both routes
// are bearer-gated, so the bound is defence in depth rather than the only control.
//
// The two routes differ in what they do at the bound, which is why they do not share a
// reader: /prompt reads through io.LimitReader and therefore truncates, while /a2a wraps
// the body in http.MaxBytesReader and refuses, because a truncated JSON body cannot parse.
const maxPromptBytes = 1 << 20

// promptHandler serves POST /prompt: it resolves the session, reads the prompt and
// runs one turn on the addressed conversation.
func (s *service) promptHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		sessionID, err := sessionIDFromRequest(r)
		if err != nil {
			http.Error(w, "invalid session id", http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, maxPromptBytes))
		if err != nil {
			http.Error(w, fmt.Sprintf("read prompt: %v", err), http.StatusBadRequest)
			return
		}

		prompt := strings.TrimSpace(string(body))
		if prompt == "" {
			http.Error(w, "empty prompt", http.StatusBadRequest)
			return
		}

		digest := sha256.Sum256([]byte(prompt))
		glog.V(2).Infof(
			"prompt intake: bytes=%d sha256=%s",
			len(prompt),
			hex.EncodeToString(digest[:8]),
		)

		entry := s.cache.Get(r.Context(), sessionID)
		result, err := entry.Prompt(r.Context(), prompt)
		if err != nil {
			glog.Warningf("prompt intake failed: %v", err)
			http.Error(w, "prompt failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// #nosec G705 -- the body is the session's own answer, served as text/plain
		// and never rendered as markup; the frozen contract fixes both the header
		// and the raw-body shape, so escaping it would break the caller.
		_, _ = fmt.Fprint(w, result)
	})
}
