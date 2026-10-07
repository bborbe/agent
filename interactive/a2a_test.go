// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/a2aproject/a2a-go/v2/a2a"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/interactive"
)

// testPublicURL is the externally reachable address the test fixtures advertise in their
// Agent Card. It is deliberately NOT the httptest server's own URL: the card's address is
// configuration, so it must never be derived from the listener.
const testPublicURL = "https://agent.example.test/a2a"

// a2aPublicURLEnv is the environment variable the service reads its public address from.
// It is spelled out here rather than imported, because the name is part of the contract.
const a2aPublicURLEnv = "A2A_PUBLIC_URL"

// newA2ATestServer builds a plain service advertising publicURL and wraps its handler in an
// httptest server.
func newA2ATestServer(
	publicURL string,
	auth interactive.Auth,
	factory agentlib.SessionFactory,
) *httptest.Server {
	svc := interactive.NewService(
		factory,
		":0",
		"",
		prometheus.NewRegistry(),
		auth,
		interactive.CardConfig{PublicURL: publicURL},
		interactive.DefaultSessionIdleTimeout,
	)
	return httptest.NewServer(svc.Handler())
}

// getAgentCard fetches the Agent Card from the well-known path with no Authorization
// header and returns the status and the raw body.
func getAgentCard(serverURL string) (int, string, error) {
	resp, err := http.Get(serverURL + "/.well-known/agent-card.json")
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", err
	}
	return resp.StatusCode, string(raw), nil
}

var _ = Describe("Agent card", func() {
	It("serves the agent card without a credential", func() {
		server := newA2ATestServer(
			testPublicURL,
			interactive.NewAuthToken(authTestToken),
			newAuthMockFactory(),
		)
		defer server.Close()

		status, body, err := getAgentCard(server.URL)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))

		var card map[string]any
		Expect(json.Unmarshal([]byte(body), &card)).To(Succeed())
		Expect(card["name"]).To(Equal("interactive"))
	})

	It("names the deployed agent and advertises its address", func() {
		const publicURL = "https://claude.example.test/a2a"
		svc := interactive.NewService(
			newAuthMockFactory(),
			":0",
			"",
			prometheus.NewRegistry(),
			interactive.NewAuthToken(authTestToken),
			interactive.CardConfig{Name: "claude-interactive", PublicURL: publicURL},
			interactive.DefaultSessionIdleTimeout,
		)
		server := httptest.NewServer(svc.Handler())
		defer server.Close()

		status, body, err := getAgentCard(server.URL)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))

		var card a2a.AgentCard
		Expect(json.Unmarshal([]byte(body), &card)).To(Succeed())
		Expect(card.Name).To(Equal("claude-interactive"))
		Expect(card.SupportedInterfaces).To(HaveLen(1))
		Expect(card.SupportedInterfaces[0].URL).To(Equal(publicURL))
	})

	It("advertises the configured public address verbatim", func() {
		const publicURL = "https://card.example.test/a2a"
		server := newA2ATestServer(
			publicURL,
			interactive.NewAuthToken(authTestToken),
			newAuthMockFactory(),
		)
		defer server.Close()

		status, body, err := getAgentCard(server.URL)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))

		var card a2a.AgentCard
		Expect(json.Unmarshal([]byte(body), &card)).To(Succeed())
		Expect(card.SupportedInterfaces).To(HaveLen(1))
		Expect(card.SupportedInterfaces[0].URL).To(Equal(publicURL))
		Expect(body).NotTo(ContainSubstring("0.0.0.0"))
	})

	It("reads the public address from the environment and fails closed", func() {
		original, had := os.LookupEnv(a2aPublicURLEnv)
		DeferCleanup(func() {
			if had {
				Expect(os.Setenv(a2aPublicURLEnv, original)).To(Succeed())
			} else {
				Expect(os.Unsetenv(a2aPublicURLEnv)).To(Succeed())
			}
		})

		Expect(os.Setenv(a2aPublicURLEnv, "https://env.example.test/a2a")).To(Succeed())
		publicURL, err := interactive.A2APublicURLFromEnv(context.Background())
		Expect(err).To(BeNil())
		Expect(publicURL).To(Equal("https://env.example.test/a2a"))

		Expect(os.Unsetenv(a2aPublicURLEnv)).To(Succeed())
		_, err = interactive.A2APublicURLFromEnv(context.Background())
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring(a2aPublicURLEnv))
	})
})
