// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery

// AgentStatus represents the outcome status of an agent task.
type AgentStatus string

const (
	// AgentStatusDone indicates the task completed successfully.
	AgentStatusDone AgentStatus = "done"
	// AgentStatusFailed indicates the task failed.
	AgentStatusFailed AgentStatus = "failed"
	// AgentStatusNeedsInput indicates the task requires additional user input.
	AgentStatusNeedsInput AgentStatus = "needs_input"
)

// AgentResultInfo holds the minimum fields a ContentGenerator needs from any agent result.
type AgentResultInfo struct {
	Status  AgentStatus
	Output  string // human-readable summary or result body
	Message string // error or status message
}
