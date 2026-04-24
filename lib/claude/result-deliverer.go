// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"context"
	"strings"

	delivery "github.com/bborbe/agent/lib/delivery"
)

//counterfeiter:generate -o ../mocks/claude-result-deliverer.go --fake-name ClaudeResultDeliverer . ResultDeliverer

// ResultDeliverer publishes a task result back after execution completes.
type ResultDeliverer[T AgentResultLike] interface {
	DeliverResult(ctx context.Context, result T) error
}

// NewResultDelivererAdapter wraps a delivery.ResultDeliverer to accept any AgentResultLike.
func NewResultDelivererAdapter[T AgentResultLike](
	inner delivery.ResultDeliverer,
) ResultDeliverer[T] {
	return &resultDelivererAdapter[T]{inner: inner}
}

type resultDelivererAdapter[T AgentResultLike] struct {
	inner delivery.ResultDeliverer
}

func (a *resultDelivererAdapter[T]) DeliverResult(ctx context.Context, result T) error {
	return a.inner.DeliverResult(ctx, delivery.AgentResultInfo{
		Status:    result.GetStatus(),
		Output:    result.RenderResultSection(),
		Message:   result.GetMessage(),
		NextPhase: result.GetNextPhase(),
	})
}

// NewNoopResultDeliverer creates a ResultDeliverer[AgentResult] that does nothing.
func NewNoopResultDeliverer() ResultDeliverer[AgentResult] {
	return NewResultDelivererAdapter[AgentResult](delivery.NewNoopResultDeliverer())
}

// BuildResultSection renders the default ## Result markdown block for any AgentResultLike.
func BuildResultSection(result AgentResultLike) string {
	var sb strings.Builder
	sb.WriteString("## Result\n\n")
	sb.WriteString("**Status:** ")
	sb.WriteString(string(result.GetStatus()))
	sb.WriteString("\n")
	if result.GetMessage() != "" {
		sb.WriteString("**Message:** ")
		sb.WriteString(result.GetMessage())
		sb.WriteString("\n")
	}
	if len(result.GetFiles()) > 0 {
		sb.WriteString("\n**Files:**\n")
		for _, f := range result.GetFiles() {
			sb.WriteString("- [[")
			sb.WriteString(f)
			sb.WriteString("]]\n")
		}
	}
	return sb.String()
}
