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
	// POST /prompt.
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
// singleton, so each binary keeps its own metrics identity.
func NewService(
	sessions agentlib.SessionFactory,
	listen string,
	providerBaseURL string,
	registry *prometheus.Registry,
) Service {
	return &service{
		cache:           newSessionCache(sessions),
		listen:          listen,
		providerBaseURL: providerBaseURL,
		registry:        registry,
	}
}

type service struct {
	cache           *sessionCache
	listen          string
	providerBaseURL string
	registry        *prometheus.Registry
}

// Handler returns the router serving readiness, metrics and prompt intake.
func (s *service) Handler() http.Handler {
	router := http.NewServeMux()
	router.Handle("/readiness", s.readinessHandler())
	router.Handle("/metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))
	router.Handle("/prompt", s.promptHandler())
	return router
}

// Run serves the handler on the configured listen address until ctx is cancelled.
func (s *service) Run(ctx context.Context) error {
	glog.V(2).Infof("starting http server listen on %s", s.listen)
	runServer := libhttp.NewServer(s.listen, s.Handler())
	return runServer(ctx)
}
