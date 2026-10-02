// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/bborbe/agent/claude"
)

// maxPermissionBytes bounds the body a single POST /permission request may carry. The
// endpoint is unauthenticated and reachable by anything in the namespace, so an
// unbounded read would let one caller exhaust the pod's memory.
const maxPermissionBytes = 64 << 10

// permissionVerdict is the POST /permission request body: the id of the request to
// resolve and the verdict to deliver to it.
type permissionVerdict struct {
	ID      string `json:"id"`
	Allow   bool   `json:"allow"`
	Message string `json:"message"`
}

// permissionHandler serves the permission endpoint. GET lists the requests currently
// waiting for a verdict; POST delivers a verdict to the request it names.
func (s *service) permissionHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.listPermissions(w)
		case http.MethodPost:
			s.resolvePermission(w, r)
		default:
			http.Error(w, "GET or POST required", http.StatusMethodNotAllowed)
		}
	})
}

// listPermissions answers GET /permission with the pending requests as a JSON array.
// The empty case is exactly [] — json.Marshal is used rather than a streaming encoder,
// which would append a newline.
//
// The pending set is plain strings, so marshalling cannot in fact fail; the error is
// still answered rather than assumed away, so a future field that can fail renders a
// 500 instead of a silent empty body.
func (s *service) listPermissions(w http.ResponseWriter) {
	payload, err := json.Marshal(s.permissions.List())
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
		return
	}
	http.Error(w, "marshal permissions", http.StatusInternalServerError)
}

// resolvePermission answers POST /permission by delivering the posted verdict to the
// request it names. An id no request holds is refused with 404 and creates nothing.
func (s *service) resolvePermission(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPermissionBytes))
	if err != nil {
		http.Error(w, "read verdict", http.StatusBadRequest)
		return
	}

	var verdict permissionVerdict
	if err := json.Unmarshal(body, &verdict); err != nil {
		http.Error(w, "invalid verdict", http.StatusBadRequest)
		return
	}
	if verdict.ID == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	resolved := s.permissions.Resolve(verdict.ID, claude.PermissionDecision{
		Allow:   verdict.Allow,
		Message: verdict.Message,
	})
	if !resolved {
		http.Error(w, "unknown permission id", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}"))
}
