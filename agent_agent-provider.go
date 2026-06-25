// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"
	"sort"

	"github.com/bborbe/errors"
)

//counterfeiter:generate -o mocks/agent-agent-provider.go --fake-name AgentAgentProvider . AgentProvider

// AgentProvider returns the *Agent registered for a given TaskType.
// Implementations are typically configured at boot via NewAgentProvider with
// a binary-specific dispatch table; the Get method is called once per task
// at the Kafka entry point.
type AgentProvider interface {
	Get(ctx context.Context, taskType TaskType) (*Agent, error)
}

// NewAgentProvider wires a task_type → *Agent dispatch table. The name argument
// identifies the consuming binary in the error message returned on a map miss
// (e.g. "agent-claude") and should match the binary's serviceName constant.
//
// The agents map is captured by reference; callers must not mutate it after
// construction. Pass a freshly-built map.
func NewAgentProvider(name string, agents map[TaskType]*Agent) AgentProvider {
	return &agentProvider{name: name, agents: agents}
}

type agentProvider struct {
	name   string
	agents map[TaskType]*Agent
}

// Get returns the *Agent registered for taskType. On a map miss, it returns
// nil and an errors.Errorf-wrapped error whose message names the offending
// task_type value, the provider's name, and the sorted list of accepted
// constants.
func (p *agentProvider) Get(ctx context.Context, taskType TaskType) (*Agent, error) {
	if agent, ok := p.agents[taskType]; ok {
		return agent, nil
	}
	accepted := make([]string, 0, len(p.agents))
	for tt := range p.agents {
		accepted = append(accepted, string(tt))
	}
	sort.Strings(accepted)
	return nil, errors.Errorf(
		ctx,
		"unknown task_type %q for %s; accepted: %v",
		taskType, p.name, accepted,
	)
}
