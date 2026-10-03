// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

import (
	"context"
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/bborbe/errors"
)

// authTokenEnv is the environment variable the service reads its bearer token from.
// The token is delivered as a runtime-only pod secret, so it is read from the process
// environment and never from a file, a manifest or a source literal.
const authTokenEnv = "INTERACTIVE_AUTH_TOKEN"

// authorizationHeader is the request header carrying the caller's bearer credential.
const authorizationHeader = "Authorization"

// bearerPrefix is the authorization scheme this service accepts.
const bearerPrefix = "Bearer "

// Auth is a service's authentication decision: either the bearer token every gated
// request must carry, or an explicit opt-out.
//
// The zero value is NOT a usable default — it refuses every gated request. A service
// built with the zero value authenticates nothing and serves only the exempt routes,
// which is the fail-closed outcome: a decision left unmade cannot silently open the
// surface. Build a real decision with NewAuthToken or AuthFromEnv, or state the
// opt-out with AuthDisabled.
type Auth struct {
	token    string
	disabled bool
}

// NewAuthToken returns the Auth that requires every gated request to present token as
// its bearer credential. An empty token is not a usable credential: it yields the same
// fail-closed value as the zero Auth.
func NewAuthToken(token string) Auth {
	return Auth{token: token}
}

// AuthDisabled is the explicit opt-out: a service built with it serves every route
// without authentication.
//
// It exists so that a consumer which is deliberately unexposed states that choice in
// one visible, greppable line rather than inheriting a default. Passing it on a pod that
// is later exposed serves that surface unauthenticated — the residual risk this design
// accepts — so grep for it before putting anything in front of the port.
var AuthDisabled = Auth{disabled: true}

// AuthFromEnv reads the bearer token from the process environment and returns the Auth
// that requires it.
//
// It returns an error when the variable is unset or empty, so a service that cannot
// authenticate fails to start rather than serving unauthenticated. The token value is
// never logged, never returned inside the error and never written anywhere.
func AuthFromEnv(ctx context.Context) (Auth, error) {
	token := os.Getenv(authTokenEnv)
	if token == "" {
		return Auth{}, errors.Errorf(ctx, "environment variable %s is unset", authTokenEnv)
	}
	return NewAuthToken(token), nil
}

// allows reports whether header carries this decision's bearer credential.
//
// The comparison is constant-time: subtle.ConstantTimeCompare returns 0 when the
// lengths differ and compares every byte otherwise, so a caller cannot recover the
// token's length or prefix from response timing. The scheme prefix is not secret and is
// compared with an ordinary prefix test.
func (a Auth) allows(header string) bool {
	if a.disabled {
		return true
	}
	if a.token == "" {
		return false
	}
	if !strings.HasPrefix(header, bearerPrefix) {
		return false
	}
	presented := strings.TrimPrefix(header, bearerPrefix)
	return subtle.ConstantTimeCompare([]byte(presented), []byte(a.token)) == 1
}

// authExempt reports whether path is served without authentication.
//
// /readiness and /metrics are exempt deliberately: a kubelet readiness probe and a
// Prometheus scrape cannot present a bearer token without the token being written into
// the pod spec's probe stanza and the scrape configuration, which multiplies the
// secret's exposure and can wedge the pod's Ready state. Neither route grants execution
// or reveals a credential. Every other path — including one registered after this
// change — is gated.
func authExempt(path string) bool {
	switch path {
	case "/readiness", "/metrics":
		return true
	default:
		return false
	}
}

// requireAuth wraps next in the service's authentication gate.
//
// Because the gate wraps the whole router, an unauthenticated request is refused before
// the wrapped handler runs: before the body is read, before a session is constructed and
// before the session lock is taken, so a refused request can neither occupy a session
// nor appear in the turn-boundary log pair.
func (s *service) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authExempt(r.URL.Path) || s.auth.allows(r.Header.Get(authorizationHeader)) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}
