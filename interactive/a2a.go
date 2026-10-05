// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

import (
	"context"
	"net/http"
	"os"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/bborbe/errors"
)

// a2aPublicURLEnv is the environment variable the service reads its externally reachable
// A2A address from. The deployed address is never the container-local listen address, so it
// is supplied as configuration and never derived from the listener or the request Host header.
const a2aPublicURLEnv = "A2A_PUBLIC_URL"

// agentCardName names this service in the Agent Card a client discovers.
const agentCardName = "interactive"

// agentCardVersion is the Agent Card's own version string. The format is the provider's to
// choose; this service has no separate release version to borrow.
const agentCardVersion = "1.0.0"

// A2APublicURLFromEnv reads the externally reachable A2A address from the process
// environment.
//
// It returns an error when the variable is unset or empty, so a service that cannot
// advertise a real endpoint fails to start rather than advertising the container-local
// listen address. The address is public by design and is not a credential.
func A2APublicURLFromEnv(ctx context.Context) (string, error) {
	publicURL := os.Getenv(a2aPublicURLEnv)
	if publicURL == "" {
		return "", errors.Errorf(ctx, "environment variable %s is unset", a2aPublicURLEnv)
	}
	return publicURL, nil
}

// newAgentCard builds the A2A Agent Card this service advertises. The single supported
// interface is the JSON-RPC binding at publicURL, which is the externally reachable address
// supplied as configuration — never the listen address and never a value derived from a
// request. The card carries no credential.
func newAgentCard(publicURL string) *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:        agentCardName,
		Version:     agentCardVersion,
		Description: "Interactive agent HTTP surface, exposed over A2A.",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(publicURL, a2a.TransportProtocolJSONRPC),
		},
		Capabilities:       a2a.AgentCapabilities{},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills: []a2a.AgentSkill{
			{
				ID:          "prompt",
				Name:        "Prompt",
				Description: "Runs one turn on an interactive agent conversation and returns the agent's reply.",
				Tags:        []string{"agent"},
			},
		},
	}
}

// agentCardHandler serves the Agent Card. The card is public by design: it advertises the
// endpoint and skills and carries no credential, so the route is exempt from the gate.
func (s *service) agentCardHandler() http.Handler {
	return a2asrv.NewStaticAgentCardHandler(s.card)
}
