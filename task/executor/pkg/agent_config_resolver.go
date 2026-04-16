// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"
	stderrors "errors"

	"github.com/bborbe/errors"
	"github.com/bborbe/k8s"

	v1 "github.com/bborbe/agent/task/executor/k8s/apis/agents.bborbe.dev/v1"
)

// ErrAgentConfigNotFound is returned by AgentConfigResolver.Resolve when no
// AgentConfig in the store has a matching Spec.Assignee.
var ErrAgentConfigNotFound = stderrors.New("agent config not found")

//counterfeiter:generate -o ../mocks/agent_config_resolver.go --fake-name FakeAgentConfigResolver . AgentConfigResolver

// AgentConfigResolver looks up the AgentConfiguration for an assignee by
// iterating the in-memory AgentConfig store and converting the matching entry.
type AgentConfigResolver interface {
	Resolve(ctx context.Context, assignee string) (AgentConfiguration, error)
}

// NewAgentConfigResolver returns an AgentConfigResolver backed by the given
// typed store. The branch is captured here and appended as the image tag at
// resolution time.
func NewAgentConfigResolver(
	provider k8s.Provider[v1.AgentConfig],
	branch string,
) AgentConfigResolver {
	return &agentConfigResolver{provider: provider, branch: branch}
}

type agentConfigResolver struct {
	provider k8s.Provider[v1.AgentConfig]
	branch   string
}

func (r *agentConfigResolver) Resolve(
	ctx context.Context,
	assignee string,
) (AgentConfiguration, error) {
	items, err := r.provider.Get(ctx)
	if err != nil {
		return AgentConfiguration{}, errors.Wrapf(ctx, err, "list agent configs")
	}
	for _, it := range items {
		if it.Spec.Assignee == assignee {
			return convert(it, r.branch), nil
		}
	}
	return AgentConfiguration{}, errors.Wrapf(
		ctx,
		ErrAgentConfigNotFound,
		"find assignee %q",
		assignee,
	)
}

func convert(obj v1.AgentConfig, branch string) AgentConfiguration {
	return AgentConfiguration{
		Assignee:        obj.Spec.Assignee,
		Image:           obj.Spec.Image + ":" + branch,
		Env:             copyEnv(obj.Spec.Env),
		SecretName:      obj.Spec.SecretName,
		VolumeClaim:     obj.Spec.VolumeClaim,
		VolumeMountPath: obj.Spec.VolumeMountPath,
	}
}

func copyEnv(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
