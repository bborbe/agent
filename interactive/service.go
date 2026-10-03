// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package interactive serves the shared interactive agent HTTP surface: readiness,
// metrics and prompt intake, over a per-session cache of long-lived conversations.
package interactive

import (
	"context"
	"net/http"

	libhttp "github.com/bborbe/http"
	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	agentlib "github.com/bborbe/agent"
)

//counterfeiter:generate -o ../mocks/interactive-service.go --fake-name InteractiveService . Service

// Service serves the interactive agent HTTP surface.
type Service interface {
	// Handler returns the HTTP handler serving GET /readiness, GET /metrics and
	// POST /prompt, plus GET and POST /permission when the service was built with a
	// permission registry. Every route except /readiness and /metrics requires the
	// configured bearer token; those two are exempt because a kubelet probe and a
	// Prometheus scrape cannot carry one without the token being written into the pod
	// spec and the scrape configuration.
	Handler() http.Handler

	// Run serves the handler on the configured listen address until ctx is cancelled.
	Run(ctx context.Context) error
}

// NewService creates the interactive session service.
//
// sessions supplies one conversation per session id; listen is the address the
// service binds; providerBaseURL is the endpoint the readiness probe dials (empty
// means the check is skipped and reported as such); registry is the Prometheus
// registry the metrics route gathers from — a parameter rather than a library
// singleton, so each binary keeps its own metrics identity; auth is the authentication
// every gated route requires, and the zero Auth refuses every gated request. The
// permission endpoint is not served by this constructor; use NewServiceWithPermissions
// to serve it.
func NewService(
	sessions agentlib.SessionFactory,
	listen string,
	providerBaseURL string,
	registry *prometheus.Registry,
	auth Auth,
) Service {
	return NewServiceWithPermissions(sessions, listen, providerBaseURL, registry, auth, nil)
}

// NewServiceWithPermissions creates the interactive session service with the
// permission endpoint enabled.
//
// permissions is the registry the endpoint serves and the decider the sessions built
// by the caller's factory consult; the caller constructs it first and passes the one
// instance to both, which is what makes the endpoint and the sessions resolve through
// the same registry. A nil permissions is invalid here — use NewService for that. auth
// is the authentication every gated route requires, and the zero Auth refuses every
// gated request.
func NewServiceWithPermissions(
	sessions agentlib.SessionFactory,
	listen string,
	providerBaseURL string,
	registry *prometheus.Registry,
	auth Auth,
	permissions PermissionRegistry,
) Service {
	return &service{
		cache:           newSessionCache(sessions),
		listen:          listen,
		providerBaseURL: providerBaseURL,
		registry:        registry,
		permissions:     permissions,
		auth:            auth,
	}
}

type service struct {
	cache           *sessionCache
	listen          string
	providerBaseURL string
	registry        *prometheus.Registry
	// permissions is the permission registry the endpoint and the sessions resolve
	// through; nil when the endpoint is not served.
	permissions PermissionRegistry
	// auth is the authentication every gated route requires; the zero value refuses
	// every gated request.
	auth Auth
}

// Handler returns the router serving readiness, metrics and prompt intake, plus the
// permission endpoint when the service was built with a registry. Every route except
// /readiness and /metrics requires the configured bearer token; those two are exempt
// because a kubelet probe and a Prometheus scrape cannot carry one without the token
// being written into the pod spec and the scrape configuration.
func (s *service) Handler() http.Handler {
	router := http.NewServeMux()
	router.Handle("/readiness", s.readinessHandler())
	router.Handle("/metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))
	router.Handle("/prompt", s.promptHandler())
	if s.permissions != nil {
		router.Handle("/permission", s.permissionHandler())
	}
	return s.requireAuth(router)
}

// Run serves the handler on the configured listen address until ctx is cancelled.
func (s *service) Run(ctx context.Context) error {
	glog.V(2).Infof("starting http server listen on %s", s.listen)
	runServer := libhttp.NewServer(s.listen, s.Handler())
	return runServer(ctx)
}
