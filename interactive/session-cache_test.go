// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	libtime "github.com/bborbe/time"
	timemocks "github.com/bborbe/time/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/bborbe/agent/interactive"
	"github.com/bborbe/agent/mocks"
)

// baseTime is the instant every cache spec's clock starts at. It is fixed rather than
// derived from the wall clock, so a spec asserts against a value it chose.
var baseTime = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// errSessionFailed is the fixed error a failing backend returns in these specs.
var errSessionFailed = errors.New("session failed")

// metricFamily finds one family in a registry's gathered output, or nil when the
// registry does not expose it. Asserting through Gather rather than reaching into the
// collector is what makes the spec observe what a scrape would see.
func metricFamily(registry *prometheus.Registry, name string) *dto.MetricFamily {
	mfs, err := registry.Gather()
	Expect(err).NotTo(HaveOccurred())
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf
		}
	}
	return nil
}

// gaugeValue reads the sole series of an unlabelled gauge family.
func gaugeValue(registry *prometheus.Registry, name string) float64 {
	mf := metricFamily(registry, name)
	Expect(mf).NotTo(BeNil(), name+" metric family not found")
	Expect(mf.Metric).To(HaveLen(1))
	return mf.Metric[0].Gauge.GetValue()
}

// counterValue reads the sole series of an unlabelled counter family.
func counterValue(registry *prometheus.Registry, name string) float64 {
	mf := metricFamily(registry, name)
	Expect(mf).NotTo(BeNil(), name+" metric family not found")
	Expect(mf.Metric).To(HaveLen(1))
	return mf.Metric[0].Counter.GetValue()
}

var _ = Describe("SessionCache", func() {
	var (
		ctx      context.Context
		registry *prometheus.Registry
		clock    *timemocks.CurrentDateTimeGetter
		session  *mocks.Session
		factory  *mocks.SessionFactory
		cache    *interactive.SessionCache
	)

	BeforeEach(func() {
		ctx = context.Background()
		registry = prometheus.NewRegistry()
		clock = &timemocks.CurrentDateTimeGetter{}
		clock.NowReturns(libtime.DateTime(baseTime))
		session = &mocks.Session{}
		session.PromptReturns("session-result", nil)
		factory = &mocks.SessionFactory{}
		factory.CreateReturns(session)
		cache = interactive.NewSessionCache(factory, time.Hour, clock, registry)
	})

	Describe("closeIdle", func() {
		It("evicts an entry idle longer than the period and closes it exactly once", func() {
			entry := cache.Get("a")
			_, err := entry.Prompt(ctx, "hello")
			Expect(err).NotTo(HaveOccurred())

			clock.NowReturns(libtime.DateTime(baseTime.Add(2 * time.Hour)))
			Expect(interactive.CloseIdle(ctx, cache)).To(Equal(1))

			Expect(session.CloseCallCount()).To(Equal(1))
		})

		It("keeps an entry used within the period", func() {
			entry := cache.Get("a")
			_, err := entry.Prompt(ctx, "hello")
			Expect(err).NotTo(HaveOccurred())

			clock.NowReturns(libtime.DateTime(baseTime.Add(30 * time.Minute)))
			Expect(interactive.CloseIdle(ctx, cache)).To(Equal(0))

			Expect(session.CloseCallCount()).To(Equal(0))
			Expect(cache.Get("a")).To(BeIdenticalTo(entry))
		})

		It("drops the entry, so Get afterwards builds a fresh conversation", func() {
			first := cache.Get("a")
			_, err := first.Prompt(ctx, "hello")
			Expect(err).NotTo(HaveOccurred())

			clock.NowReturns(libtime.DateTime(baseTime.Add(2 * time.Hour)))
			Expect(interactive.CloseIdle(ctx, cache)).To(Equal(1))

			second := cache.Get("a")
			Expect(second).NotTo(BeIdenticalTo(first))
			Expect(factory.CreateCallCount()).To(Equal(2))
		})

		It("counts an erroring turn as use, not as idleness", func() {
			session.PromptReturns("", errSessionFailed)
			entry := cache.Get("a")
			_, err := entry.Prompt(ctx, "hello")
			Expect(err).To(MatchError(errSessionFailed))

			clock.NowReturns(libtime.DateTime(baseTime.Add(30 * time.Minute)))
			Expect(interactive.CloseIdle(ctx, cache)).To(Equal(0))
			Expect(session.CloseCallCount()).To(Equal(0))
			Expect(cache.Get("a")).To(BeIdenticalTo(entry))
		})

		It("skips an entry whose turn is in flight and leaves it in the cache", func() {
			release := make(chan struct{})
			started := make(chan struct{})
			var once sync.Once
			session.PromptStub = func(_ context.Context, _ string) (string, error) {
				once.Do(func() { close(started) })
				<-release
				return "ok", nil
			}

			entry := cache.Get("a")
			turnDone := make(chan struct{})
			go func() {
				defer close(turnDone)
				_, _ = entry.Prompt(ctx, "hello")
			}()
			Eventually(started).Should(BeClosed())

			// The clock is far past the period, so the only thing keeping the entry
			// alive is that its turn is still running.
			clock.NowReturns(libtime.DateTime(baseTime.Add(2 * time.Hour)))
			Expect(interactive.CloseIdle(ctx, cache)).To(Equal(0))
			Expect(session.CloseCallCount()).To(Equal(0))
			Expect(cache.Get("a")).To(BeIdenticalTo(entry))

			// Releasing the turn must not retroactively evict it: the close is never
			// deferred to the end of the turn.
			close(release)
			Eventually(turnDone).Should(BeClosed())
			Expect(session.CloseCallCount()).To(Equal(0))
			Expect(cache.Get("a")).To(BeIdenticalTo(entry))
		})

		It("keeps sweeping when one session's Close fails", func() {
			session.CloseReturns(errSessionFailed)
			_, _ = cache.Get("a").Prompt(ctx, "hello")

			clock.NowReturns(libtime.DateTime(baseTime.Add(2 * time.Hour)))
			Expect(interactive.CloseIdle(ctx, cache)).To(Equal(1))
			Expect(session.CloseCallCount()).To(Equal(1))
		})
	})

	Describe("idle period normalisation", func() {
		It("keeps a positive period", func() {
			Expect(interactive.IdleTimeout(cache)).To(Equal(time.Hour))
		})

		DescribeTable("replaces a non-positive period with the documented default",
			func(configured time.Duration) {
				normalised := interactive.NewSessionCache(
					factory,
					configured,
					clock,
					prometheus.NewRegistry(),
				)
				Expect(interactive.IdleTimeout(normalised)).
					To(Equal(interactive.DefaultSessionIdleTimeout))
			},
			Entry("zero", time.Duration(0)),
			Entry("negative", -time.Minute),
		)

		It("still evicts when the period was non-positive", func() {
			normalised := interactive.NewSessionCache(
				factory,
				0,
				clock,
				prometheus.NewRegistry(),
			)
			entry := normalised.Get("a")
			_, err := entry.Prompt(ctx, "hello")
			Expect(err).NotTo(HaveOccurred())

			clock.NowReturns(
				libtime.DateTime(baseTime.Add(interactive.DefaultSessionIdleTimeout)),
			)
			Expect(interactive.CloseIdle(ctx, normalised)).To(Equal(0))

			clock.NowReturns(
				libtime.DateTime(
					baseTime.Add(interactive.DefaultSessionIdleTimeout + time.Second),
				),
			)
			Expect(interactive.CloseIdle(ctx, normalised)).To(Equal(1))
			Expect(session.CloseCallCount()).To(Equal(1))
		})
	})

	Describe("metrics", func() {
		It("reports the live entry count and the eviction count", func() {
			Expect(gaugeValue(registry, "interactive_sessions_held")).To(Equal(0.0))
			Expect(
				counterValue(registry, "interactive_sessions_evicted_total"),
			).To(Equal(0.0))

			_, _ = cache.Get("a").Prompt(ctx, "hello")
			_, _ = cache.Get("b").Prompt(ctx, "hello")
			Expect(gaugeValue(registry, "interactive_sessions_held")).To(Equal(2.0))

			clock.NowReturns(libtime.DateTime(baseTime.Add(2 * time.Hour)))
			Expect(interactive.CloseIdle(ctx, cache)).To(Equal(2))
			Expect(gaugeValue(registry, "interactive_sessions_held")).To(Equal(0.0))
			Expect(
				counterValue(registry, "interactive_sessions_evicted_total"),
			).To(Equal(2.0))
		})
	})

	Describe("reap", func() {
		It("evicts idle entries on a tick", func() {
			_, _ = cache.Get("a").Prompt(ctx, "hello")
			clock.NowReturns(libtime.DateTime(baseTime.Add(2 * time.Hour)))

			reapCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { done <- interactive.Reap(reapCtx, cache, 5*time.Millisecond) }()

			Eventually(func() float64 {
				return counterValue(registry, "interactive_sessions_evicted_total")
			}, 2*time.Second).Should(Equal(1.0))
			Expect(session.CloseCallCount()).To(Equal(1))

			cancel()
			Eventually(done, 2*time.Second).Should(Receive(BeNil()))
		})

		It("sweeps once per tick", func() {
			// The entry never becomes idle, so each tick reaches the clock exactly
			// once and the call count is the number of sweeps that have run.
			_, _ = cache.Get("a").Prompt(ctx, "hello")

			reapCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { done <- interactive.Reap(reapCtx, cache, 5*time.Millisecond) }()

			Eventually(clock.NowCallCount, 2*time.Second).
				Should(BeNumerically(">=", 3))

			cancel()
			Eventually(done, 2*time.Second).Should(Receive(BeNil()))
		})

		It("returns when the context is cancelled", func() {
			reapCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { done <- interactive.Reap(reapCtx, cache, time.Hour) }()

			cancel()
			Eventually(done, 2*time.Second).Should(Receive(BeNil()))
		})
	})
})

var _ = Describe("Run reaper wiring", func() {
	It("starts the idle-session reaper", func() {
		session := &mocks.Session{}
		session.PromptReturns("session-result", nil)
		factory := &mocks.SessionFactory{}
		factory.CreateReturns(session)

		// A one-millisecond idle period against the real clock, with the reaper's
		// own period shortened through the test shim: the entry is idle by the time
		// the first sweep runs. This is what fails if Run never starts the goroutine
		// — the eviction logic itself is exercised by the direct closeIdle specs.
		svc := interactive.NewService(
			factory,
			":0",
			"",
			prometheus.NewRegistry(),
			interactive.AuthDisabled,
			interactive.CardConfig{PublicURL: testPublicURL},
			time.Millisecond,
		)
		interactive.SetReapInterval(svc, 5*time.Millisecond)

		server := httptest.NewServer(svc.Handler())
		defer server.Close()
		status, err := postPrompt(server.URL, "abc", "hello")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- svc.Run(ctx) }()
		defer func() {
			cancel()
			Eventually(done, 5*time.Second).Should(Receive(BeNil()))
		}()

		Eventually(session.CloseCallCount, 5*time.Second).Should(Equal(1))
	})
})
