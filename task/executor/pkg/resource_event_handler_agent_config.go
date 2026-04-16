// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"

	"github.com/bborbe/k8s"
	"k8s.io/client-go/tools/cache"

	v1 "github.com/bborbe/agent/task/executor/k8s/apis/agents.bborbe.dev/v1"
)

// NewResourceEventHandlerAgentConfig adapts an EventHandlerAgentConfig to the
// cache.ResourceEventHandler the informer expects.
func NewResourceEventHandlerAgentConfig(
	ctx context.Context,
	handler EventHandlerAgentConfig,
) cache.ResourceEventHandler {
	return k8s.NewResourceEventHandler[v1.AgentConfig](ctx, handler)
}
