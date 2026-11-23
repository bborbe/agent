// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import "strings"

// taskTypedFields are the task-frontmatter keys the vault API validates and
// reasons about — the ones whose values it checks against a transition graph,
// a normalised vocabulary or a rule. It is not the whole of what a write may
// name: the agent pipeline's own keys are admitted too, through
// taskPipelineFields below, and the two are kept apart deliberately.
var taskTypedFields = map[string]bool{
	"status": true, "phase": true, "assignee": true, "task_type": true,
	"stage": true, "cluster": true, "flag": true, "goals": true,
	"priority": true, "category": true, "role": true, "mode": true,
	"defer_date": true, "due_date": true, "target_date": true,
	"planned_date": true, "start_date": true, "blocked_by": true,
	"themes": true, "page_type": true,
}

// taskPipelineFields are the task-frontmatter keys the agent pipeline writes
// through the vault API. A key here is one the API stores and carries without
// interpreting it; a key in taskTypedFields is one it validates. Merging the
// two would hide that difference from the next reader.
//
// max_retries is carried rather than written by this repository: a result
// PATCH is a read-modify-write, so an operator-set max_retries rides along in
// the payload, and the vault API refuses the whole request because the key is
// neither enumerated there nor metrics_-prefixed. It is the one key in this
// list the API did not already accept.
var taskPipelineFields = map[string]bool{
	"trigger_scope": true, "trigger_count": true, "max_triggers": true,
	"current_job": true, "claude_session_id": true, "job_started_at": true,
	"spawn_notification": true, "queued_at": true, "aborted_reason": true,
	"gate_successor": true, "target_vault": true, "previous_assignee": true,
	"retry_count": true, "last_result_status": true, "completed_date": true,
	"processed_at": true, "superseded_by": true, "base_ref": true,
	"clone_url": true, "ref": true, "repo": true, "current_version": true,
	"override_reason": true, "title": true, "created_by": true,
	// created is stamped by recurring-task-creator's templates — the AI
	// Knowledge Sharing one sets it so vault-cli's closure gate does not
	// grandfather every instance. The API stores it without interpreting it;
	// no typed creation date exists. Measured 2026-11-22 on
	// agent-task-controller-seibert-personal-0: the create answered
	// 400 unknown_field field=created and was dropped whole, so the weekly
	// instance never materialized.
	"created":          true,
	"auto_abort_prior": true, "date": true, "launcher": true,
	"branch": true, "pr_number": true, "current_go": true, "latest_go": true,
	"episode_sha": true, "vuln_count": true, "vulns": true,
	"short_id": true, "project": true, "issue_url": true,
	"new_version": true, "previous_version": true, "release_kind": true,
	"release_notes_url": true, "recovery_sha": true, "run_outcome": true,
	"failure_reason": true, "run_id": true,
	"max_retries": true,
}

// taskPatchFields is the union of taskTypedFields and taskPipelineFields, built
// once at package scope so the two source sets are the only place the keys are
// written down. A key is admitted when it is in this union or carries the
// metrics_ prefix.
var taskPatchFields = buildTaskPatchFields()

// buildTaskPatchFields combines the typed and pipeline sets into the canonical
// allowlist. It is called once, at package initialisation.
func buildTaskPatchFields() map[string]bool {
	fields := make(map[string]bool, len(taskTypedFields)+len(taskPipelineFields))
	for key := range taskTypedFields {
		fields[key] = true
	}
	for key := range taskPipelineFields {
		fields[key] = true
	}
	return fields
}

// metricsFieldPrefix is the one open namespace: the run-record keys belong to
// the executor and the controller, so a new counter must be admitted without a
// change here. The enumerated sets stay closed; every other unknown key is
// still refused.
const metricsFieldPrefix = "metrics_"

// TaskPatchFieldAdmitted reports whether a task PATCH may name this frontmatter
// key. Call this instead of testing a set directly: it is the rule, and a
// caller that tests only the enumerated sets misses the one open namespace.
//
// The metrics_ prefix is that namespace — the vault API admits every metrics_*
// key by prefix rather than enumerating them, because the key list belongs to
// the executor and the controller and a new counter must not require an API
// release before a verdict can be published. Every other unknown key is
// refused.
func TaskPatchFieldAdmitted(key string) bool {
	return taskPatchFields[key] || strings.HasPrefix(key, metricsFieldPrefix)
}

// TaskTypedFields returns a fresh copy of the task-frontmatter keys the vault
// API validates and reasons about. This package is their single definition,
// though neither consumer calls it yet — until they adopt this package the two
// repositories still keep their own copies.
// A key absent from this set is not necessarily refused — the pipeline's keys
// are admitted through TaskPipelineFields, and metrics_* through its prefix.
//
// A key the predicate rejects is refused by the vault API, and a client must
// drop such a key rather than send it: one rejected key refuses the whole
// request and loses every field in it.
func TaskTypedFields() map[string]bool {
	return copyFields(taskTypedFields)
}

// TaskPipelineFields returns a fresh copy of the task-frontmatter keys the agent
// pipeline writes through the vault API. This package is their single definition,
// though neither consumer calls it yet — until they adopt this package the two
// repositories still keep their own copies.
// A key absent from this set is not necessarily refused — the API's own typed
// keys are admitted through TaskTypedFields, and metrics_* through its prefix.
//
// A key the predicate rejects is refused by the vault API, and a client must
// drop such a key rather than send it: one rejected key refuses the whole
// request and loses every field in it.
func TaskPipelineFields() map[string]bool {
	return copyFields(taskPipelineFields)
}

// TaskPatchFields returns a fresh copy of the complete set of enumerated
// task-frontmatter keys a task PATCH may name — the typed keys the vault API
// validates plus the pipeline keys it carries. This package is the single
// definition of that rule.
//
// It is intended for the vault API's request validation and the task
// controller's result writer; this package is the one rule they should call
// rather than copy. Neither calls it yet. The metrics_* namespace is admitted
// by prefix and is deliberately not enumerated here, so use
// TaskPatchFieldAdmitted for the whole rule.
//
// A key the predicate rejects is refused by the vault API, and a client must
// drop such a key rather than send it: one rejected key refuses the whole
// request and loses every field in it.
func TaskPatchFields() map[string]bool {
	return copyFields(taskPatchFields)
}

// copyFields returns an independent copy of fields, so a caller cannot mutate
// the canonical set.
func copyFields(fields map[string]bool) map[string]bool {
	out := make(map[string]bool, len(fields))
	for key, admitted := range fields {
		out[key] = admitted
	}
	return out
}
