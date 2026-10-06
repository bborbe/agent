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

// defaultAgentCardName names this service in the Agent Card a client discovers when the
// caller supplies no name of its own.
const defaultAgentCardName = "interactive"

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

// CardConfig is what the A2A Agent Card advertises about this service.
type CardConfig struct {
	// Name names the deployed agent in the Agent Card. Empty means the card keeps
	// the library default, "interactive".
	Name string
	// PublicURL is the externally reachable A2A endpoint the card advertises
	// verbatim. It is never derived from the listen address or a request header.
	PublicURL string
}

// newAgentCard builds the A2A Agent Card this service advertises. The single supported
// interface is the JSON-RPC binding at config.PublicURL, which is the externally reachable
// address supplied as configuration — never the listen address and never a value derived
// from a request. config.Name names the deployed agent, falling back to
// defaultAgentCardName when it is empty. The card carries no credential.
func newAgentCard(config CardConfig) *a2a.AgentCard {
	name := config.Name
	if name == "" {
		name = defaultAgentCardName
	}
	return &a2a.AgentCard{
		Name:        name,
		Version:     agentCardVersion,
		Description: "Interactive agent HTTP surface, exposed over A2A.",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(config.PublicURL, a2a.TransportProtocolJSONRPC),
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
