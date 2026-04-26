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
	// AgentStatusInProgress indicates the agent has completed a step within the current phase
	// and saved partial state, but the phase is not yet complete. The controller writes the update
	// without advancing the phase. Used by multi-step phase handlers for in-place progress saves.
	// NextPhase is ignored on this status.
	AgentStatusInProgress AgentStatus = "in_progress"
)

// AgentResultInfo holds the minimum fields a ContentGenerator needs from any agent result.
type AgentResultInfo struct {
	Status  AgentStatus
	Output  string // human-readable summary or result body
	Message string // error or status message
	// NextPhase is the task phase the agent requests the controller to write
	// when Status == AgentStatusDone. Ignored on Failed/NeedsInput (failure
	// paths always escalate to human_review). Empty means "use default"
	// (phase: done on Status: done). Valid values are vault-cli TaskPhase
	// enum strings: planning, in_progress, ai_review, human_review, done.
	NextPhase string
}
