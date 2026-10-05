// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive_test

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/interactive"
	"github.com/bborbe/agent/mocks"
)

var _ = BeforeSuite(func() {
	Expect(flag.Set("logtostderr", "true")).To(Succeed())
	Expect(flag.Set("v", "2")).To(Succeed())
})

// captureStderr redirects the process's stderr into a buffer for the duration of fn
// and returns what was written. glog's stderr sink reads os.Stderr when it emits, so
// the swap is observed as long as -logtostderr is on.
func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, err := os.Pipe()
	Expect(err).To(BeNil())
	os.Stderr = w
	defer func() { os.Stderr = old }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	Expect(w.Close()).To(Succeed())
	os.Stderr = old
	return <-done
}

// newTestServer builds the shared service through its constructor, explicitly
// unauthenticated, and wraps its handler in an httptest server.
func newTestServer(factory agentlib.SessionFactory, providerBaseURL string) *httptest.Server {
	svc := interactive.NewService(
		factory,
		":0",
		providerBaseURL,
		prometheus.NewRegistry(),
		interactive.AuthDisabled,
		testPublicURL,
	)
	return httptest.NewServer(svc.Handler())
}

// postPrompt sends one prompt to the service and returns the response status.
func postPrompt(serverURL, sessionID, body string) (int, error) {
	req, err := http.NewRequest(http.MethodPost, serverURL+"/prompt", strings.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Session-Id", sessionID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func strptr(value string) *string { return &value }

// contractObserver reports what a frozen-contract row needs to know about the
// backend's interactions: which session ids the factory was asked for, and what
// the session was prompted with. It is the seam that lets one contract table drive
// any session backend, not just the counterfeiter fake.
type contractObserver interface {
	// CreateCount reports how many sessions the factory has built.
	CreateCount() int
	// CreateArg reports the id the factory was asked for at index i.
	CreateArg(i int) string
	// PromptArg reports the prompt the session received at index i.
	PromptArg(i int) string
}

// contractBackend supplies the session backend a contract run drives and the
// observer its rows assert against.
type contractBackend struct {
	factory  agentlib.SessionFactory
	observer contractObserver
}

// mockObserver reports the interactions of a counterfeiter session fake.
type mockObserver struct {
	factory *mocks.SessionFactory
	session *mocks.Session
}

func (o *mockObserver) CreateCount() int { return o.factory.CreateCallCount() }

func (o *mockObserver) CreateArg(i int) string { return o.factory.CreateArgsForCall(i) }

func (o *mockObserver) PromptArg(i int) string {
	_, prompt := o.session.PromptArgsForCall(i)
	return prompt
}

// newMockBackend builds a fresh mock-backed contract backend for one row.
func newMockBackend() *contractBackend {
	session := &mocks.Session{}
	session.PromptReturns("session-result", nil)
	factory := &mocks.SessionFactory{}
	factory.CreateReturns(session)
	return &contractBackend{
		factory:  factory,
		observer: &mockObserver{factory: factory, session: session},
	}
}

// runContractTable drives the frozen-contract rows against whatever backend
// newBackend supplies. It is the one driver: a second backend is wired in by
// passing a different newBackend, never by copying the rows.
func runContractTable(description string, newBackend func() *contractBackend) {
	body := func(
		method string,
		path string,
		headerValue *string,
		body string,
		providerBaseURL string,
		verify func(resp *http.Response, body string, obs contractObserver),
	) {
		backend := newBackend()
		server := newTestServer(backend.factory, providerBaseURL)
		defer server.Close()

		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, server.URL+path, reader)
		Expect(err).To(BeNil())
		if headerValue != nil {
			req.Header.Set("X-Session-Id", *headerValue)
		}

		resp, err := http.DefaultClient.Do(req)
		Expect(err).To(BeNil())
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		Expect(err).To(BeNil())

		verify(resp, string(raw), backend.observer)
	}

	DescribeTable(description, append([]any{body}, contractEntries()...)...)
}

// contractEntries is the single source of the frozen-contract rows.
func contractEntries() []any {
	return append(routeEntries(), promptEntries()...)
}

// routeEntries are the contract rows that never reach a session.
func routeEntries() []any {
	return []any{
		Entry(
			"GET /readiness with PROVIDER_BASE_URL unset",
			http.MethodGet,
			"/readiness",
			nil,
			"",
			"",
			func(resp *http.Response, body string, _ contractObserver) {
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(body).To(Equal(
					"OK (PROVIDER_BASE_URL unset — provider reachability not checked)",
				))
			},
		),
		Entry(
			"GET /readiness with an unroutable provider",
			http.MethodGet,
			"/readiness",
			nil,
			"",
			"http://127.0.0.1:1",
			func(resp *http.Response, _ string, _ contractObserver) {
				Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
			},
		),
		Entry(
			"GET /metrics",
			http.MethodGet,
			"/metrics",
			nil,
			"",
			"",
			func(resp *http.Response, _ string, _ contractObserver) {
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			},
		),
		Entry(
			"GET /prompt is rejected",
			http.MethodGet,
			"/prompt",
			nil,
			"",
			"",
			func(resp *http.Response, _ string, _ contractObserver) {
				Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
			},
		),
		Entry(
			"GET /permission without a permission registry",
			http.MethodGet,
			"/permission",
			nil,
			"",
			"",
			func(resp *http.Response, _ string, _ contractObserver) {
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			},
		),
	}
}

// promptEntries are the contract rows that reach a session.
func promptEntries() []any {
	return []any{
		Entry(
			"POST /prompt without a session header",
			http.MethodPost,
			"/prompt",
			nil,
			"hello",
			"",
			func(resp *http.Response, _ string, obs contractObserver) {
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(obs.CreateCount()).To(Equal(1))
				Expect(obs.CreateArg(0)).To(Equal("identity"))
			},
		),
		Entry(
			"POST /prompt with an empty session header",
			http.MethodPost,
			"/prompt",
			strptr(""),
			"hello",
			"",
			func(resp *http.Response, _ string, _ contractObserver) {
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			},
		),
		Entry(
			"POST /prompt with a malformed session header",
			http.MethodPost,
			"/prompt",
			strptr("bad id!"),
			"hello",
			"",
			func(resp *http.Response, _ string, _ contractObserver) {
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			},
		),
		Entry(
			"POST /prompt with an oversized body",
			http.MethodPost,
			"/prompt",
			nil,
			strings.Repeat("a", 2<<20),
			"",
			func(resp *http.Response, _ string, obs contractObserver) {
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(len(obs.PromptArg(0))).To(Equal(1 << 20))
			},
		),
		Entry(
			"POST /prompt with an empty body",
			http.MethodPost,
			"/prompt",
			nil,
			"",
			"",
			func(resp *http.Response, _ string, _ contractObserver) {
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			},
		),
		Entry(
			"POST /prompt with a valid body",
			http.MethodPost,
			"/prompt",
			nil,
			"hello",
			"",
			func(resp *http.Response, body string, _ contractObserver) {
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(resp.Header.Get("Content-Type")).
					To(Equal("text/plain; charset=utf-8"))
				Expect(body).To(Equal("session-result"))
			},
		),
	}
}

var _ = Describe("Service", func() {
	It("serves all three routes", func() {
		session := &mocks.Session{}
		session.PromptReturns("session-result", nil)
		factory := &mocks.SessionFactory{}
		factory.CreateReturns(session)
		server := newTestServer(factory, "")
		defer server.Close()

		readiness, err := http.Get(server.URL + "/readiness")
		Expect(err).To(BeNil())
		defer readiness.Body.Close()
		Expect(readiness.StatusCode).To(Equal(http.StatusOK))

		metrics, err := http.Get(server.URL + "/metrics")
		Expect(err).To(BeNil())
		defer metrics.Body.Close()
		Expect(metrics.StatusCode).To(Equal(http.StatusOK))

		status, err := postPrompt(server.URL, "abc", "hello")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
	})

	runContractTable("frozen :9090 contract", newMockBackend)

	Describe("per-session locking", func() {
		It("distinct ids overlap", func() {
			release := make(chan struct{})
			started := make(chan string, 2)
			factory := &mocks.SessionFactory{}
			factory.CreateStub = func(id string) agentlib.Session {
				session := &mocks.Session{}
				session.PromptStub = func(
					_ context.Context,
					_ string,
				) (string, error) {
					started <- id
					<-release
					return "ok-" + id, nil
				}
				return session
			}
			server := newTestServer(factory, "")
			defer server.Close()

			statuses := make([]int, 2)
			errs := make([]error, 2)
			var wg sync.WaitGroup
			for i, id := range []string{"a", "b"} {
				wg.Add(1)
				go func(i int, id string) {
					defer wg.Done()
					statuses[i], errs[i] = postPrompt(server.URL, id, "hello")
				}(i, id)
			}

			Eventually(started).Should(Receive())
			Eventually(started).Should(Receive())
			close(release)
			wg.Wait()

			Expect(errs[0]).To(BeNil())
			Expect(errs[1]).To(BeNil())
			Expect(statuses).To(Equal([]int{http.StatusOK, http.StatusOK}))
		})

		It("same id serialises", func() {
			release := make(chan struct{})
			started := make(chan struct{}, 2)
			factory := &mocks.SessionFactory{}
			factory.CreateStub = func(_ string) agentlib.Session {
				session := &mocks.Session{}
				session.PromptStub = func(
					_ context.Context,
					_ string,
				) (string, error) {
					started <- struct{}{}
					<-release
					return "ok", nil
				}
				return session
			}
			server := newTestServer(factory, "")
			defer server.Close()

			statuses := make([]int, 2)
			errs := make([]error, 2)
			var wg sync.WaitGroup

			wg.Add(1)
			go func() {
				defer wg.Done()
				statuses[0], errs[0] = postPrompt(server.URL, "abc", "one")
			}()
			Eventually(started).Should(Receive())

			wg.Add(1)
			go func() {
				defer wg.Done()
				statuses[1], errs[1] = postPrompt(server.URL, "abc", "two")
			}()

			Consistently(started, 200*time.Millisecond).ShouldNot(Receive())
			Expect(factory.CreateCallCount()).To(Equal(1))

			close(release)
			wg.Wait()
			Eventually(started).Should(Receive())

			Expect(errs[0]).To(BeNil())
			Expect(errs[1]).To(BeNil())
			Expect(statuses).To(Equal([]int{http.StatusOK, http.StatusOK}))
		})
	})

	Describe("turn boundary logging", func() {
		It("turn pair emitted", func() {
			session := &mocks.Session{}
			session.PromptReturns("answer", nil)
			factory := &mocks.SessionFactory{}
			factory.CreateReturns(session)
			server := newTestServer(factory, "")
			defer server.Close()

			var status int
			var err error
			out := captureStderr(func() {
				status, err = postPrompt(server.URL, "abc", "hello")
			})
			Expect(err).To(BeNil())
			Expect(status).To(Equal(http.StatusOK))

			start := strings.Index(out, "turn start id=abc")
			end := strings.Index(out, "turn end id=abc")
			Expect(start).To(BeNumerically(">=", 0))
			Expect(end).To(BeNumerically(">=", 0))
			Expect(start).To(BeNumerically("<", end))
		})

		It("no turn line for rejected request", func() {
			factory := &mocks.SessionFactory{}
			factory.CreateReturns(&mocks.Session{})
			server := newTestServer(factory, "")
			defer server.Close()

			cases := []func() (int, error){
				func() (int, error) {
					req, err := http.NewRequest(
						http.MethodPost,
						server.URL+"/prompt",
						strings.NewReader("hello"),
					)
					if err != nil {
						return 0, err
					}
					req.Header.Set("X-Session-Id", "bad id!")
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						return 0, err
					}
					defer resp.Body.Close()
					_, _ = io.Copy(io.Discard, resp.Body)
					return resp.StatusCode, nil
				},
				func() (int, error) {
					req, err := http.NewRequest(
						http.MethodPost,
						server.URL+"/prompt",
						strings.NewReader("hello"),
					)
					if err != nil {
						return 0, err
					}
					req.Header.Set("X-Session-Id", "")
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						return 0, err
					}
					defer resp.Body.Close()
					_, _ = io.Copy(io.Discard, resp.Body)
					return resp.StatusCode, nil
				},
				func() (int, error) {
					return postPrompt(server.URL, "abc", "")
				},
				func() (int, error) {
					resp, err := http.Get(server.URL + "/prompt")
					if err != nil {
						return 0, err
					}
					defer resp.Body.Close()
					_, _ = io.Copy(io.Discard, resp.Body)
					return resp.StatusCode, nil
				},
			}

			for _, call := range cases {
				out := captureStderr(func() {
					_, _ = call()
				})
				Expect(out).NotTo(ContainSubstring("turn start"))
			}
		})
	})

	Describe("failure modes", func() {
		It("releases the session lock when the runner panics", func() {
			var calls int32
			session := &mocks.Session{}
			session.PromptStub = func(_ context.Context, _ string) (string, error) {
				if atomic.AddInt32(&calls, 1) == 1 {
					panic("boom")
				}
				return "second", nil
			}
			factory := &mocks.SessionFactory{}
			factory.CreateReturns(session)
			server := newTestServer(factory, "")
			defer server.Close()

			_, _ = postPrompt(server.URL, "abc", "one")

			status, err := postPrompt(server.URL, "abc", "two")
			Expect(err).To(BeNil())
			Expect(status).To(Equal(http.StatusOK))
		})

		It("returns 500 and still closes the turn when the runner fails", func() {
			session := &mocks.Session{}
			session.PromptReturns("", errors.New("boom"))
			factory := &mocks.SessionFactory{}
			factory.CreateReturns(session)
			server := newTestServer(factory, "")
			defer server.Close()

			var status int
			var body string
			var err error
			out := captureStderr(func() {
				var resp *http.Response
				req, reqErr := http.NewRequest(
					http.MethodPost,
					server.URL+"/prompt",
					strings.NewReader("hello"),
				)
				if reqErr != nil {
					err = reqErr
					return
				}
				req.Header.Set("X-Session-Id", "abc")
				resp, err = http.DefaultClient.Do(req)
				if err != nil {
					return
				}
				defer resp.Body.Close()
				status = resp.StatusCode
				raw, readErr := io.ReadAll(resp.Body)
				err = readErr
				body = string(raw)
			})
			Expect(err).To(BeNil())
			Expect(status).To(Equal(http.StatusInternalServerError))
			Expect(body).To(Equal("prompt failed\n"))
			Expect(out).To(ContainSubstring("turn start id=abc"))
			Expect(out).To(ContainSubstring("turn end id=abc"))
		})

		It("keeps one cache entry per id and never evicts", func() {
			session := &mocks.Session{}
			session.PromptReturns("session-result", nil)
			factory := &mocks.SessionFactory{}
			factory.CreateReturns(session)
			server := newTestServer(factory, "")
			defer server.Close()

			_, err := postPrompt(server.URL, "a", "hello")
			Expect(err).To(BeNil())
			_, err = postPrompt(server.URL, "b", "hello")
			Expect(err).To(BeNil())
			_, err = postPrompt(server.URL, "a", "hello")
			Expect(err).To(BeNil())

			Expect(factory.CreateCallCount()).To(Equal(2))
			Expect(factory.CreateArgsForCall(0)).To(Equal("a"))
			Expect(factory.CreateArgsForCall(1)).To(Equal("b"))
		})

		It("keeps serving prompts when the provider is unreachable", func() {
			session := &mocks.Session{}
			session.PromptReturns("session-result", nil)
			factory := &mocks.SessionFactory{}
			factory.CreateReturns(session)
			server := newTestServer(factory, "http://127.0.0.1:1")
			defer server.Close()

			resp, err := http.Get(server.URL + "/readiness")
			Expect(err).To(BeNil())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))

			status, err := postPrompt(server.URL, "abc", "hello")
			Expect(err).To(BeNil())
			Expect(status).To(Equal(http.StatusOK))
		})
	})
})

var _ = Describe("Readiness", func() {
	It("reports the provider reachable when it answers a dial", func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).To(BeNil())
		defer listener.Close()
		go func() {
			for {
				conn, acceptErr := listener.Accept()
				if acceptErr != nil {
					return
				}
				_ = conn.Close()
			}
		}()

		server := newTestServer(&mocks.SessionFactory{}, "http://"+listener.Addr().String())
		defer server.Close()

		resp, err := http.Get(server.URL + "/readiness")
		Expect(err).To(BeNil())
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		Expect(err).To(BeNil())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(string(raw)).
			To(Equal("OK (provider reachable at " + listener.Addr().String() + ")"))
	})

	DescribeTable("dial address derivation",
		func(providerBaseURL string, expectedAddr string) {
			server := newTestServer(&mocks.SessionFactory{}, providerBaseURL)
			defer server.Close()

			resp, err := http.Get(server.URL + "/readiness")
			Expect(err).To(BeNil())
			defer resp.Body.Close()
			raw, err := io.ReadAll(resp.Body)
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
			Expect(string(raw)).To(ContainSubstring(expectedAddr))
		},
		Entry("defaults an http port to 80", "http://127.0.0.1", "127.0.0.1:80"),
		Entry("defaults an https port to 443", "https://127.0.0.1", "127.0.0.1:443"),
		Entry("treats a bare value as host:port", "127.0.0.1:1", "127.0.0.1:1"),
		Entry("rejects an unparsable url", "http://[::1", "parse provider base url"),
	)
})

var _ = Describe("Run", func() {
	It("serves until the context is cancelled", func() {
		svc := interactive.NewService(
			&mocks.SessionFactory{},
			":0",
			"",
			prometheus.NewRegistry(),
			interactive.AuthDisabled,
			testPublicURL,
		)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- svc.Run(ctx) }()

		time.Sleep(100 * time.Millisecond)
		cancel()

		Eventually(done, 5*time.Second).Should(Receive(BeNil()))
	})
})
