// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"github.com/bborbe/k8s"

	v1 "github.com/bborbe/agent/task/executor/k8s/apis/agents.bborbe.dev/v1"
)

// EventHandlerAgentConfig is the typed in-memory event handler / store
// for AgentConfig resources. Backed by github.com/bborbe/k8s generics.
type EventHandlerAgentConfig k8s.EventHandler[v1.AgentConfig]

// NewEventHandlerAgentConfig returns an empty EventHandlerAgentConfig.
func NewEventHandlerAgentConfig() EventHandlerAgentConfig {
	return k8s.NewEventHandler[v1.AgentConfig]()
}
