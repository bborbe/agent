// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bborbe/errors"
)

// readinessDialTimeout bounds the provider dial a readiness probe performs.
const readinessDialTimeout = 5 * time.Second

// readinessHandler serves GET /readiness: it dials the configured provider so the
// probe answers "can this agent reach its provider at all" rather than "is the
// process alive".
func (s *service) readinessHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.providerBaseURL == "" {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(
				w,
				"OK (PROVIDER_BASE_URL unset — provider reachability not checked)",
			)
			return
		}

		addr, err := dialAddress(r.Context(), s.providerBaseURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		conn, err := net.DialTimeout("tcp", addr, readinessDialTimeout)
		if err != nil {
			http.Error(
				w,
				fmt.Sprintf("provider unreachable at %s: %v", addr, err),
				http.StatusServiceUnavailable,
			)
			return
		}
		_ = conn.Close()

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "OK (provider reachable at %s)", addr)
	})
}

// dialAddress turns a provider base URL into a host:port to dial, defaulting the port
// from the scheme so "https://host" and "host" both work.
func dialAddress(ctx context.Context, raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.Wrapf(ctx, err, "parse provider base url %q", raw)
	}
	host := parsed.Host
	if host == "" {
		// No scheme — treat the whole value as host[:port].
		host = raw
	}
	if !strings.Contains(host, ":") {
		if parsed.Scheme == "http" {
			host += ":80"
		} else {
			host += ":443"
		}
	}
	return host, nil
}
