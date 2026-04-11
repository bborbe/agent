// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

// AgentConfiguration defines the container image and environment for one agent type.
type AgentConfiguration struct {
	// Assignee is the task frontmatter assignee value that routes to this agent.
	Assignee string
	// Image is the container image base name (without tag). Tag is appended at runtime from branch.
	Image string
	// Env holds per-agent environment variables (e.g. API keys, config).
	// These are merged with shared env vars (TASK_CONTENT, TASK_ID, KAFKA_BROKERS, BRANCH)
	// when spawning the K8s Job.
	Env map[string]string
}

// AgentConfigurations is a list of agent configurations.
type AgentConfigurations []AgentConfiguration

// FindByAssignee returns the configuration for the given assignee name.
// Returns the config and true if found, zero value and false otherwise.
func (a AgentConfigurations) FindByAssignee(assignee string) (AgentConfiguration, bool) {
	for _, c := range a {
		if c.Assignee == assignee {
			return c, true
		}
	}
	return AgentConfiguration{}, false
}

// TaggedConfigurations returns a new AgentConfigurations with the branch appended
// to each image as a tag (e.g. "registry/image" + ":" + "dev" → "registry/image:dev").
func (a AgentConfigurations) TaggedConfigurations(branch string) AgentConfigurations {
	result := make(AgentConfigurations, len(a))
	for i, c := range a {
		result[i] = AgentConfiguration{
			Assignee: c.Assignee,
			Image:    c.Image + ":" + branch,
			Env:      c.Env,
		}
	}
	return result
}
