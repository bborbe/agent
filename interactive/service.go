// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package interactive serves the shared interactive agent HTTP surface: readiness,
// metrics and prompt intake, over a per-session cache of long-lived conversations.
package interactive

import (
	"context"
	"net/http"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	libhttp "github.com/bborbe/http"
	libtime "github.com/bborbe/time"
	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	agentlib "github.com/bborbe/agent"
)

//counterfeiter:generate -o ../mocks/interactive-service.go --fake-name InteractiveService . Service

// reapInterval is how often the idle-session reaper sweeps the cache. It is a period of
// its own, not a fraction of the configured idle timeout: the sweep is cheap, and tying
// it to the timeout would make a long timeout delay the release of a process that is
// already idle by any measure.
const reapInterval = 30 * time.Second

// Service serves the interactive agent HTTP surface.
type Service interface {
	// Handler returns the HTTP handler serving GET /readiness, GET /metrics and
	// POST /prompt, plus POST /a2a (the A2A JSON-RPC binding), plus GET and POST
	// /permission when the service was built with a permission registry, plus the A2A
	// Agent Card at the well-known path. Every route except /readiness, /metrics and
	// the Agent Card requires the configured bearer token; those three are exempt
	// because a kubelet probe and a Prometheus scrape cannot carry one without the
	// token being written into the pod spec and the scrape configuration, and
	// discovery of the card is public by design.
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
// every gated route requires, and the zero Auth refuses every gated request; card names
// the agent in the Agent Card and carries the externally reachable address the card
// advertises, which is never derived from listen; sessionIdleTimeout is how long a
// session may go without serving a turn before its conversation is closed and dropped
// from the cache, and a non-positive value is replaced with DefaultSessionIdleTimeout
// rather than disabling eviction. The permission endpoint is not served by this
// constructor; use NewServiceWithPermissions to serve it.
func NewService(
	sessions agentlib.SessionFactory,
	listen string,
	providerBaseURL string,
	registry *prometheus.Registry,
	auth Auth,
	card CardConfig,
	sessionIdleTimeout time.Duration,
) Service {
	return NewServiceWithPermissions(
		sessions,
		listen,
		providerBaseURL,
		registry,
		auth,
		card,
		nil,
		sessionIdleTimeout,
	)
}

// NewServiceWithPermissions creates the interactive session service with the
// permission endpoint enabled.
//
// permissions is the registry the endpoint serves and the decider the sessions built
// by the caller's factory consult; the caller constructs it first and passes the one
// instance to both, which is what makes the endpoint and the sessions resolve through
// the same registry. A nil permissions is invalid here — use NewService for that. auth
// is the authentication every gated route requires, and the zero Auth refuses every
// gated request. card names the agent in the Agent Card and carries the externally
// reachable address the card advertises, which is never derived from listen.
// sessionIdleTimeout is how long a session may go without serving a turn before its
// conversation is closed and dropped from the cache; a non-positive value is replaced
// with DefaultSessionIdleTimeout rather than disabling eviction, so the bound cannot be
// switched off by a misconfigured period.
func NewServiceWithPermissions(
	sessions agentlib.SessionFactory,
	listen string,
	providerBaseURL string,
	registry *prometheus.Registry,
	auth Auth,
	card CardConfig,
	permissions PermissionRegistry,
	sessionIdleTimeout time.Duration,
) Service {
	return &service{
		cache: newSessionCache(
			sessions,
			sessionIdleTimeout,
			libtime.NewCurrentDateTime(),
			registry,
		),
		listen:          listen,
		providerBaseURL: providerBaseURL,
		registry:        registry,
		permissions:     permissions,
		auth:            auth,
		card:            newAgentCard(card),
		reapInterval:    reapInterval,
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
	// card is the Agent Card served at the well-known path. Its interface URL is the
	// configured public address, so the card can never advertise the container-local
	// listen address.
	card *a2a.AgentCard
	// reapInterval is how often Run's reaper sweeps the cache for idle sessions. It
	// is initialised from the reapInterval constant and is a field rather than the
	// constant read inline so a test can shorten it and observe that Run actually
	// starts the reaper.
	reapInterval time.Duration
}

// Handler returns the router serving readiness, metrics, prompt intake, the A2A JSON-RPC
// binding and the A2A Agent Card, plus the permission endpoint when the service was built
// with a registry. Every route except /readiness, /metrics and the Agent Card requires the
// configured bearer token — including /a2a, which the gate refuses with 401 before the
// handler runs; those three are exempt because a kubelet probe and a Prometheus scrape
// cannot carry one without the token being written into the pod spec and the scrape
// configuration, and discovery of the card is public by design.
func (s *service) Handler() http.Handler {
	router := http.NewServeMux()
	router.Handle("/readiness", s.readinessHandler())
	router.Handle("/metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))
	router.Handle("/prompt", s.promptHandler())
	router.Handle(a2asrv.WellKnownAgentCardPath, s.agentCardHandler())
	router.Handle(a2aPath, s.a2aHandler())
	if s.permissions != nil {
		router.Handle("/permission", s.permissionHandler())
	}
	return s.requireAuth(router)
}

// serverOptionFns returns the option closures the interactive HTTP server is built
// with. The write deadline lives here rather than inline in Run so the external test
// package can assert it through github.com/bborbe/http without a request that has to
// outlive the library's 30-second default.
func serverOptionFns() []func(*libhttp.ServerOptions) {
	return []func(*libhttp.ServerOptions){
		func(o *libhttp.ServerOptions) {
			o.WriteTimeout = 10 * time.Minute
		},
	}
}

// Run serves the handler on the configured listen address until ctx is cancelled.
//
// It also starts the idle-session reaper, which closes and drops a conversation that has
// gone the configured period without serving a turn. The reaper observes the same ctx
// and stops with the server, so a cancelled Run leaks neither the goroutine nor the
// sessions it holds.
func (s *service) Run(ctx context.Context) error {
	glog.V(2).Infof("starting http server listen on %s", s.listen)
	go func() {
		if err := s.cache.reap(ctx, s.reapInterval); err != nil {
			glog.Warningf("session reaper stopped: %v", err)
		}
	}()
	// One server-wide write deadline governs all six routes this service registers —
	// /readiness, /metrics, /prompt, /.well-known/agent-card.json, /a2a and /permission —
	// because libhttp.NewServer builds a single http.Server from a single
	// libhttp.ServerOptions value: there is no per-route deadline, so raising it for one
	// route raises it for every route, and no route can keep the 30-second default while
	// another is raised.
	//
	// The ten minutes buy the three turn-holding routes — POST /prompt, POST /a2a and
	// GET/POST /permission — each of which runs one agent turn whose provider answers in
	// tens of seconds. Go sets the write deadline when the request headers are read, so a
	// handler that outlives the deadline reaches a deadline that has already passed and
	// cannot write its response; the turn completes server-side and the answer is
	// silently lost. Measured against the deployed claude-interactive service: 27.9s
	// returned 200, while 32.5s and above returned 502.
	//
	// /readiness and /metrics are acceptable at the longer cap because both handlers
	// return immediately, so the longer deadline can only bite a client that stops
	// reading a response the handler has already produced.
	runServer := libhttp.NewServer(s.listen, s.Handler(), serverOptionFns()...)
	return runServer(ctx)
}
