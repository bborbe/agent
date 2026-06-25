// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

// AgentStatus represents the outcome status of a step (or single-shot agent).
type AgentStatus string

const (
	// AgentStatusDone indicates the step completed successfully.
	// On the last step of a phase, set NextPhase to advance.
	// On a mid-phase step, leave NextPhase empty (in-place save).
	AgentStatusDone AgentStatus = "done"

	// AgentStatusInProgress indicates the step completed and saved partial state,
	// but the phase is not yet complete. Phase frontmatter is preserved.
	// Used by multi-step phases for in-place progress saves between steps.
	// NextPhase is ignored on this status.
	AgentStatusInProgress AgentStatus = "in_progress"

	// AgentStatusFailed indicates a transient infrastructure failure.
	// Controller retries (trigger_count++); after max_triggers, escalates.
	AgentStatusFailed AgentStatus = "failed"

	// AgentStatusNeedsInput indicates a semantic problem in the task body.
	// Routed straight to human_review — retrying won't help.
	AgentStatusNeedsInput AgentStatus = "needs_input"
)

// AgentResultInfo holds the minimum fields a deliverer needs to publish
// a step's result. ResultDeliverer.DeliverResult takes this directly.
type AgentResultInfo struct {
	Status  AgentStatus
	Output  string // body content (typically heading + fenced JSON)
	Message string // human-readable status; used by failure/needs_input paths
	// NextPhase is the task phase the agent requests the controller to write
	// when Status == AgentStatusDone. Ignored on Failed/NeedsInput (failure
	// paths always escalate to human_review). Empty means "use default"
	// (phase: done on Status: done). Valid values are vault-cli TaskPhase
	// enum strings: planning, in_progress, ai_review, human_review, done.
	NextPhase string
}
