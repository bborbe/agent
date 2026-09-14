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
	// paths always escalate to human_review). Empty means "stay in current
	// phase" — an in-place save between steps of a multi-step phase; the
	// task keeps status: in_progress and its phase untouched. Terminating a
	// task requires an explicit NextPhase: "done". Valid values are vault-cli
	// TaskPhase enum strings: planning, execution, ai_review, human_review,
	// done ("in_progress" is a legacy alias for execution).
	NextPhase string
	// ContinueToNext mirrors Result.ContinueToNext: whether the StepRunner
	// proceeds to the next step in the same Job invocation. Informational
	// for deliverers — a Done result with empty NextPhase is an in-place
	// save regardless of this flag.
	ContinueToNext bool
	// AgentTurns is the number of conversation turns the session that produced this
	// result took, from the CLI's own end-of-run summary. Nil means no measurement was
	// reported — a zero or negative summary is treated as no measurement, and the turn
	// entry is then omitted from the payload entirely.
	AgentTurns *int64
	// InteractionCount is the number of human-authored entries the run's own session
	// transcript recorded. Nil means the evidence was unavailable — the interaction
	// entry is omitted from the payload. A non-nil zero is an observation: the
	// transcript was read and contained no human-authored entry. Absence is never
	// substituted with a zero.
	InteractionCount *int64
}
