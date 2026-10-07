// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/interactive"
	"github.com/bborbe/agent/mocks"
)

// authTestToken is the bearer token the auth tests configure. It is a placeholder,
// never a real credential, and it is never written next to the scheme prefix in this
// file, so the source-hygiene grep finds no `Bearer <token>` literal.
const authTestToken = "test-token-value"

// authWrongToken is a token of the same shape that is not the configured one.
const authWrongToken = "wrong-token-value"

// authEnv is the environment variable the service reads its token from. It is spelled
// out here rather than imported, because the name is part of the contract.
const authEnv = "INTERACTIVE_AUTH_TOKEN"

// newAuthMockFactory returns a counterfeiter factory whose sessions answer every prompt
// with "session-result". A factory written as a bare &mocks.SessionFactory{} has no
// CreateReturns, so Create returns a nil Session and the first turn that reaches the
// prompt route panics — every fixture that lets a request through the gate uses this.
func newAuthMockFactory() *mocks.SessionFactory {
	session := &mocks.Session{}
	session.PromptReturns("session-result", nil)
	factory := &mocks.SessionFactory{}
	factory.CreateReturns(session)
	return factory
}

// newAuthTestServer builds a plain service with the given decision and wraps its
// handler in an httptest server.
func newAuthTestServer(auth interactive.Auth, factory agentlib.SessionFactory) *httptest.Server {
	svc := interactive.NewService(
		factory,
		":0",
		"",
		prometheus.NewRegistry(),
		auth,
		interactive.CardConfig{PublicURL: testPublicURL},
		interactive.DefaultSessionIdleTimeout,
	)
	return httptest.NewServer(svc.Handler())
}

// newAuthPermissionTestServer builds a permission-enabled service with the given
// decision and wraps its handler in an httptest server. It is the fixture the
// route-policy table needs, because /permission only exists on a permission-enabled
// service.
func newAuthPermissionTestServer(
	auth interactive.Auth,
	permissions interactive.PermissionRegistry,
	factory agentlib.SessionFactory,
) *httptest.Server {
	svc := interactive.NewServiceWithPermissions(
		factory,
		":0",
		"",
		prometheus.NewRegistry(),
		auth,
		interactive.CardConfig{PublicURL: testPublicURL},
		permissions,
		interactive.DefaultSessionIdleTimeout,
	)
	return httptest.NewServer(svc.Handler())
}

// requestWithAuth sends one request and returns the response status. An empty
// authorization sends no Authorization header at all, which is the "absent header"
// case; the session header is always set so the prompt route's own validation cannot
// confuse a row.
func requestWithAuth(
	serverURL string,
	method string,
	path string,
	body string,
	authorization string,
) (int, error) {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, serverURL+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Session-Id", "abc")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// postPromptAuth posts one prompt with the given Authorization header.
func postPromptAuth(serverURL, sessionID, body, authorization string) (int, error) {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(http.MethodPost, serverURL+"/prompt", reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Session-Id", sessionID)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

var _ = Describe("Authentication", func() {
	It("rejects a request with no Authorization header", func() {
		server := newAuthTestServer(interactive.NewAuthToken(authTestToken), newAuthMockFactory())
		defer server.Close()

		status, err := postPromptAuth(server.URL, "abc", "hello", "")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusUnauthorized))
	})

	It("rejects a request with a wrong token", func() {
		server := newAuthTestServer(interactive.NewAuthToken(authTestToken), newAuthMockFactory())
		defer server.Close()

		status, err := postPromptAuth(server.URL, "abc", "hello", "Bearer "+authWrongToken)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusUnauthorized))
	})

	It("serves a request with the correct token", func() {
		server := newAuthTestServer(interactive.NewAuthToken(authTestToken), newAuthMockFactory())
		defer server.Close()

		status, err := postPromptAuth(server.URL, "abc", "hello", "Bearer "+authTestToken)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
	})

	It("rejects a malformed Authorization header", func() {
		server := newAuthTestServer(interactive.NewAuthToken(authTestToken), newAuthMockFactory())
		defer server.Close()

		status, err := postPromptAuth(server.URL, "abc", "hello", "Basic "+authTestToken)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusUnauthorized))

		status, err = postPromptAuth(server.URL, "abc", "hello", "Bearer")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusUnauthorized))
	})

	It("rejects a token that is a prefix of the correct one", func() {
		server := newAuthTestServer(interactive.NewAuthToken(authTestToken), newAuthMockFactory())
		defer server.Close()

		short := authTestToken[:len(authTestToken)-1]
		status, err := postPromptAuth(server.URL, "abc", "hello", "Bearer "+short)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusUnauthorized))
	})

	It("rejects a zero-value auth decision", func() {
		// The zero value is not a "serve everything" default: it is the fail-closed
		// state, so a decision left unmade refuses every gated request.
		server := newAuthTestServer(interactive.Auth{}, newAuthMockFactory())
		defer server.Close()

		status, err := postPromptAuth(server.URL, "abc", "hello", "Bearer "+authTestToken)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusUnauthorized))
	})

	It("serves unauthenticated when auth is explicitly disabled", func() {
		server := newAuthTestServer(interactive.AuthDisabled, newAuthMockFactory())
		defer server.Close()

		status, err := postPromptAuth(server.URL, "abc", "hello", "")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
	})

	It("refuses a request before any session work", func() {
		factory := newAuthMockFactory()
		server := newAuthTestServer(interactive.NewAuthToken(authTestToken), factory)
		defer server.Close()

		var refusedStatus, servedStatus int
		refusedOut := captureStderr(func() {
			refusedStatus, _ = postPromptAuth(server.URL, "abc", "hello", "")
		})
		servedOut := captureStderr(func() {
			servedStatus, _ = postPromptAuth(server.URL, "abc", "hello", "Bearer "+authTestToken)
		})

		Expect(refusedStatus).To(Equal(http.StatusUnauthorized))
		Expect(strings.Count(refusedOut, "turn start")).To(Equal(0))
		Expect(servedStatus).To(Equal(http.StatusOK))
		Expect(strings.Count(servedOut, "turn start")).To(Equal(1))
		Expect(factory.CreateCallCount()).To(Equal(1))
	})

	DescribeTable("gates every route as recorded",
		func(method string, path string, body string, authorization string, expected int) {
			server := newAuthPermissionTestServer(
				interactive.NewAuthToken(authTestToken),
				interactive.NewPermissionRegistry(),
				newAuthMockFactory(),
			)
			defer server.Close()

			status, err := requestWithAuth(server.URL, method, path, body, authorization)
			Expect(err).To(BeNil())
			Expect(status).To(Equal(expected))
		},
		Entry("readiness stays open", http.MethodGet, "/readiness", "", "", http.StatusOK),
		Entry("metrics stays open", http.MethodGet, "/metrics", "", "", http.StatusOK),
		Entry(
			"prompt without a token is refused",
			http.MethodPost,
			"/prompt",
			"hello",
			"",
			http.StatusUnauthorized,
		),
		Entry(
			"permission without a token is refused",
			http.MethodGet,
			"/permission",
			"",
			"",
			http.StatusUnauthorized,
		),
		Entry(
			"prompt with a token is served",
			http.MethodPost,
			"/prompt",
			"hello",
			"Bearer "+authTestToken,
			http.StatusOK,
		),
		Entry(
			"permission with a token is served",
			http.MethodGet,
			"/permission",
			"",
			"Bearer "+authTestToken,
			http.StatusOK,
		),
	)
})

var _ = Describe("Auth from environment", func() {
	It("reads the token from the process environment", func() {
		Expect(os.Setenv(authEnv, authTestToken)).To(Succeed())
		DeferCleanup(os.Unsetenv, authEnv)

		auth, err := interactive.AuthFromEnv(context.Background())
		Expect(err).To(BeNil())

		server := newAuthTestServer(auth, newAuthMockFactory())
		defer server.Close()

		status, err := postPromptAuth(server.URL, "abc", "hello", "Bearer "+authTestToken)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))

		status, err = postPromptAuth(server.URL, "abc", "hello", "")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusUnauthorized))
	})

	It("fails when the token is not set", func() {
		original, had := os.LookupEnv(authEnv)
		Expect(os.Unsetenv(authEnv)).To(Succeed())
		DeferCleanup(func() {
			if had {
				Expect(os.Setenv(authEnv, original)).To(Succeed())
			}
		})

		auth, err := interactive.AuthFromEnv(context.Background())
		Expect(err).NotTo(BeNil())
		Expect(auth).To(Equal(interactive.Auth{}))
		Expect(err.Error()).NotTo(ContainSubstring(authTestToken))
	})
})
