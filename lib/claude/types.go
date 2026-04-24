// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	delivery "github.com/bborbe/agent/lib/delivery"
)

// AgentStatus is the shared agent status type.
type AgentStatus = delivery.AgentStatus

const (
	// AgentStatusDone indicates the task completed successfully.
	AgentStatusDone = delivery.AgentStatusDone
	// AgentStatusFailed indicates the task failed.
	AgentStatusFailed = delivery.AgentStatusFailed
	// AgentStatusNeedsInput indicates the task requires additional user input.
	AgentStatusNeedsInput = delivery.AgentStatusNeedsInput
)

// AgentResult is the structured output written to stdout for the task/executor to read.
type AgentResult struct {
	Status    AgentStatus `json:"status"`
	Message   string      `json:"message,omitempty"`
	Files     []string    `json:"files,omitempty"`
	NextPhase string      `json:"next_phase,omitempty"`
}

func (r AgentResult) GetStatus() AgentStatus { return r.Status }

func (r AgentResult) GetMessage() string { return r.Message }

func (r AgentResult) GetFiles() []string { return r.Files }

// GetNextPhase returns the requested next task phase, or empty string for default behavior.
func (r AgentResult) GetNextPhase() string { return r.NextPhase }

func (r AgentResult) RenderResultSection() string { return BuildResultSection(r) }

// AgentResultLike is the constraint for types that can be delivered as task results.
type AgentResultLike interface {
	GetStatus() AgentStatus
	GetMessage() string
	GetFiles() []string
	GetNextPhase() string
	RenderResultSection() string
}
