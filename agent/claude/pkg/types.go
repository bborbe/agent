// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

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
	Status  AgentStatus `json:"status"`
	Message string      `json:"message,omitempty"`
	Files   []string    `json:"files,omitempty"`
}
