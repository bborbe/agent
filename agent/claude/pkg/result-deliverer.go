// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"
	"strings"

	delivery "github.com/bborbe/agent/lib/delivery"
)

//counterfeiter:generate -o ../mocks/pkg-result-deliverer.go --fake-name PkgResultDeliverer . ResultDeliverer

// ResultDeliverer publishes an AgentResult back after task execution completes.
type ResultDeliverer interface {
	DeliverResult(ctx context.Context, result AgentResult) error
}

// NewResultDelivererAdapter wraps a delivery.ResultDeliverer to accept agent-claude AgentResult.
func NewResultDelivererAdapter(inner delivery.ResultDeliverer) ResultDeliverer {
	return &resultDelivererAdapter{inner: inner}
}

type resultDelivererAdapter struct {
	inner delivery.ResultDeliverer
}

func (a *resultDelivererAdapter) DeliverResult(ctx context.Context, result AgentResult) error {
	return a.inner.DeliverResult(ctx, delivery.AgentResultInfo{
		Status:  result.Status,
		Output:  BuildResultSection(result),
		Message: result.Message,
	})
}

// NewNoopResultDeliverer creates a ResultDeliverer that does nothing.
func NewNoopResultDeliverer() ResultDeliverer {
	return NewResultDelivererAdapter(delivery.NewNoopResultDeliverer())
}

// BuildResultSection creates a markdown section from an AgentResult.
func BuildResultSection(result AgentResult) string {
	var sb strings.Builder
	sb.WriteString("## Result\n\n")
	sb.WriteString("**Status:** ")
	sb.WriteString(string(result.Status))
	sb.WriteString("\n")
	if result.Message != "" {
		sb.WriteString("**Message:** ")
		sb.WriteString(result.Message)
		sb.WriteString("\n")
	}
	if len(result.Files) > 0 {
		sb.WriteString("\n**Files:**\n")
		for _, f := range result.Files {
			sb.WriteString("- [[")
			sb.WriteString(f)
			sb.WriteString("]]\n")
		}
	}
	return sb.String()
}
