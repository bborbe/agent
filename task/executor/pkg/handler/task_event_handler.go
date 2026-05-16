// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libtime "github.com/bborbe/time"
	"github.com/bborbe/vault-cli/pkg/domain"
	"github.com/golang/glog"

	lib "github.com/bborbe/agent/lib"
	pkg "github.com/bborbe/agent/task/executor/pkg"
	"github.com/bborbe/agent/task/executor/pkg/metrics"
	"github.com/bborbe/agent/task/executor/pkg/spawner"
)

// defaultTriggerPhases is the fallback phase allow-list when the per-Config Trigger is absent or empty.
var defaultTriggerPhases = domain.TaskPhases{
	domain.TaskPhasePlanning,
	domain.TaskPhaseInProgress,
	domain.TaskPhaseAIReview,
}

// defaultTriggerStatuses is the fallback status allow-list when the per-Config Trigger is absent or empty.
var defaultTriggerStatuses = domain.TaskStatuses{
	domain.TaskStatusInProgress,
}

// defaultRespawnGracePeriod is the window after job_started_at during which the executor
// suppresses respawn when the K8s Job is inactive but no terminal phase has been observed.
// The window gives the agent's terminal-phase write time to propagate through the vault pipeline.
const defaultRespawnGracePeriod = 300 * time.Second

// terminalPhases is the set of phases that must never trigger a new spawn.
// Extending this set requires a follow-up spec if vault-cli adds new terminal phases.
var terminalPhases = map[domain.TaskPhase]struct{}{
	domain.TaskPhaseHumanReview: {},
	domain.TaskPhaseDone:        {},
}

// knownPhases contains all phase constants exported by vault-cli v0.64.0.
// Values outside this set trigger enum-drift logging (event=unknown_phase).
var knownPhases = map[domain.TaskPhase]struct{}{
	domain.TaskPhaseTodo:        {},
	domain.TaskPhasePlanning:    {},
	domain.TaskPhaseInProgress:  {},
	domain.TaskPhaseAIReview:    {},
	domain.TaskPhaseHumanReview: {},
	domain.TaskPhaseDone:        {},
}

// IsTerminal reports whether the given phase is in the terminal set.
// Tasks at a terminal phase must not be re-spawned; operator intervention is required.
func IsTerminal(phase domain.TaskPhase) bool {
	_, ok := terminalPhases[phase]
	return ok
}

// applyPhaseGate emits metrics/logs for terminal and unknown phases.
// Returns true when the task must be skipped (terminal phase suppressed).
func applyPhaseGate(task lib.Task, phase domain.TaskPhase) bool {
	if IsTerminal(phase) {
		glog.Infof("event=spawn_suppressed phase=%s task=%s", phase, task.TaskIdentifier)
		metrics.TaskEventsTotal.WithLabelValues("spawn_suppressed_terminal_phase").Inc()
		return true
	}
	if _, inKnown := knownPhases[phase]; !inKnown {
		glog.Infof("event=unknown_phase phase=%s task=%s", phase, task.TaskIdentifier)
		metrics.TaskEventsTotal.WithLabelValues("unknown_phase").Inc()
	}
	return false
}

//counterfeiter:generate -o ../../mocks/task_event_handler.go --fake-name FakeTaskEventHandler . TaskEventHandler

// TaskEventHandler processes a single task event message from Kafka.
type TaskEventHandler interface {
	ConsumeMessage(ctx context.Context, msg *sarama.ConsumerMessage) error
}

// NewTaskEventHandler creates a new TaskEventHandler.
func NewTaskEventHandler(
	jobSpawner spawner.JobSpawner,
	branch base.Branch,
	resolver pkg.ConfigResolver,
	resultPublisher pkg.ResultPublisher,
	taskStore *pkg.TaskStore,
	currentDateTime libtime.CurrentDateTimeGetter,
) TaskEventHandler {
	return &taskEventHandler{
		jobSpawner:      jobSpawner,
		branch:          branch,
		resolver:        resolver,
		resultPublisher: resultPublisher,
		taskStore:       taskStore,
		currentDateTime: currentDateTime,
	}
}

type taskEventHandler struct {
	jobSpawner      spawner.JobSpawner
	branch          base.Branch
	resolver        pkg.ConfigResolver
	resultPublisher pkg.ResultPublisher
	taskStore       *pkg.TaskStore
	currentDateTime libtime.CurrentDateTimeGetter
}

func (h *taskEventHandler) ConsumeMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	task, config, skip, err := h.parseAndFilter(ctx, msg)
	if err != nil {
		return err
	}
	if skip {
		return nil
	}
	return h.spawnIfNeeded(ctx, task, config)
}

// parseAndFilter unmarshals the message and applies all pre-spawn filter checks.
// Returns (task, config, true, nil) when the message should be silently skipped.
// Returns (task, config, false, nil) when the task qualifies for spawning.
// Returns (_, _, false, err) when an unexpected error occurred.
func (h *taskEventHandler) parseAndFilter(
	ctx context.Context,
	msg *sarama.ConsumerMessage,
) (lib.Task, *pkg.AgentConfiguration, bool, error) {
	if len(msg.Value) == 0 {
		glog.V(3).Infof("skip empty message at offset %d", msg.Offset)
		return lib.Task{}, nil, true, nil
	}

	var task lib.Task
	if err := json.Unmarshal(msg.Value, &task); err != nil {
		glog.Warningf("failed to unmarshal task event at offset %d: %v", msg.Offset, err)
		return lib.Task{}, nil, true, nil
	}

	if task.TaskIdentifier == "" {
		glog.Warningf("task event at offset %d has empty TaskIdentifier, skipping", msg.Offset)
		return lib.Task{}, nil, true, nil
	}

	// Clean up taskStore for completed tasks so the job informer does not emit
	// a spurious synthetic failure after the agent has already published success.
	if string(task.Frontmatter.Status()) == "completed" {
		h.taskStore.Delete(task.TaskIdentifier)
		glog.V(3).Infof("task %s completed: removed from task store", task.TaskIdentifier)
	}

	// Resolve the per-agent Config before the status/phase checks so both filters
	// can use the per-Config trigger. Skip lookup when assignee is empty.
	config, skip, err := h.resolveConfig(ctx, task)
	if err != nil {
		return lib.Task{}, nil, false, err
	}
	if skip {
		return lib.Task{}, nil, true, nil
	}

	// Type filter: effective set = {cfg.TaskType} ∪ cfg.TaskTypes.
	// Skipped when the effective set is empty (agent has no task types configured).
	if mismatch := taskTypeMismatchReason(task, config); mismatch != "" {
		if err := h.resultPublisher.PublishTypeMismatchFailure(ctx, task, mismatch); err != nil {
			metrics.TaskEventsTotal.WithLabelValues("error").Inc()
			return lib.Task{}, nil, false, errors.Wrapf(
				ctx, err, "publish type mismatch failure for task %s", task.TaskIdentifier,
			)
		}
		glog.V(2).Infof("type mismatch: %s (task %s)", mismatch, task.TaskIdentifier)
		metrics.TaskEventsTotal.WithLabelValues("type_mismatch").Inc()
		return lib.Task{}, nil, true, nil
	}

	if !effectiveTriggerStatuses(config).Contains(task.Frontmatter.Status()) {
		glog.V(3).Infof(
			"skip task %s with status %s", task.TaskIdentifier, task.Frontmatter.Status(),
		)
		metrics.TaskEventsTotal.WithLabelValues("skipped_status").Inc()
		return lib.Task{}, nil, true, nil
	}

	phase := task.Frontmatter.Phase()

	// terminal phases must not be spawned again — operator escalation required
	if phase != nil && applyPhaseGate(task, *phase) {
		return lib.Task{}, nil, true, nil
	}
	if phase == nil || !effectiveTriggerPhases(config).Contains(*phase) {
		glog.V(3).Infof("skip task %s with phase %v", task.TaskIdentifier, phase)
		metrics.TaskEventsTotal.WithLabelValues("skipped_phase").Inc()
		return lib.Task{}, nil, true, nil
	}

	stage := task.Frontmatter.Stage()
	if stage != string(h.branch) {
		glog.V(3).Infof(
			"skip task %s with stage %s (executor branch %s)",
			task.TaskIdentifier, stage, h.branch,
		)
		metrics.TaskEventsTotal.WithLabelValues("skipped_stage").Inc()
		return lib.Task{}, nil, true, nil
	}

	if task.Frontmatter.Assignee() == "" {
		glog.V(3).Infof("skip task %s with empty assignee", task.TaskIdentifier)
		metrics.TaskEventsTotal.WithLabelValues("skipped_assignee").Inc()
		return lib.Task{}, nil, true, nil
	}

	return task, config, false, nil
}

// resolveConfig looks up the agent Config CR for the task's assignee.
// Returns (nil, false, nil) when assignee is empty (caller handles the empty-assignee path).
// Returns (nil, true, nil) when the assignee is unknown (ErrConfigNotFound).
// Returns (nil, false, err) on unexpected resolver errors.
func (h *taskEventHandler) resolveConfig(
	ctx context.Context,
	task lib.Task,
) (*pkg.AgentConfiguration, bool, error) {
	if task.Frontmatter.Assignee() == "" {
		return nil, false, nil
	}
	resolved, err := h.resolver.Resolve(ctx, string(task.Frontmatter.Assignee()))
	if err != nil {
		if stderrors.Is(err, pkg.ErrConfigNotFound) {
			glog.Warningf(
				"skip task %s: unknown assignee %s",
				task.TaskIdentifier,
				task.Frontmatter.Assignee(),
			)
			metrics.TaskEventsTotal.WithLabelValues("skipped_unknown_assignee").Inc()
			return nil, true, nil
		}
		metrics.TaskEventsTotal.WithLabelValues("error").Inc()
		return nil, false, errors.Wrapf(
			ctx,
			err,
			"resolve agent config for task %s",
			task.TaskIdentifier,
		)
	}
	return &resolved, false, nil
}

// taskTypeMismatchReason returns a non-empty reason string when the task's task_type is not in the
// agent's effective type set. Returns "" when the filter passes (match or effective set is empty).
func taskTypeMismatchReason(task lib.Task, cfg *pkg.AgentConfiguration) string {
	if cfg == nil {
		return ""
	}
	effectiveTypes := pkg.EffectiveTaskTypes(cfg.TaskType, cfg.TaskTypes)
	if len(effectiveTypes) == 0 {
		return ""
	}
	taskType := task.Frontmatter.TaskType()
	if pkg.TaskTypeInSet(string(taskType), effectiveTypes) {
		return ""
	}
	if taskType == "" {
		return fmt.Sprintf(
			"task has no task_type; agent %q accepts %v",
			cfg.Assignee,
			effectiveTypes,
		)
	}
	return fmt.Sprintf(
		"task_type %q not in effective set %v of agent %q",
		taskType, effectiveTypes, cfg.Assignee,
	)
}

// effectiveTriggerPhases returns the phase allow-list from the Config trigger,
// falling back to defaultTriggerPhases when Trigger is absent or the list is empty.
func effectiveTriggerPhases(cfg *pkg.AgentConfiguration) domain.TaskPhases {
	if cfg == nil || cfg.Trigger == nil || len(cfg.Trigger.Phases) == 0 {
		return defaultTriggerPhases
	}
	return cfg.Trigger.Phases
}

// effectiveTriggerStatuses returns the status allow-list from the Config trigger,
// falling back to defaultTriggerStatuses when Trigger is absent or the list is empty.
func effectiveTriggerStatuses(cfg *pkg.AgentConfiguration) domain.TaskStatuses {
	if cfg == nil || cfg.Trigger == nil || len(cfg.Trigger.Statuses) == 0 {
		return defaultTriggerStatuses
	}
	return cfg.Trigger.Statuses
}

// checkActiveCurrentJob verifies whether spawn must be suppressed due to current_job state.
// Returns (true, nil) when the spawn must be suppressed (job still active or inside grace window).
// Returns (false, nil) when spawn may proceed. Returns (false, err) on unexpected errors.
func (h *taskEventHandler) checkActiveCurrentJob(
	ctx context.Context,
	task lib.Task,
	currentJob string,
) (bool, error) {
	active, err := h.jobSpawner.IsJobActive(ctx, task.TaskIdentifier)
	if err != nil {
		metrics.TaskEventsTotal.WithLabelValues("error").Inc()
		return false, errors.Wrapf(
			ctx,
			err,
			"check current_job active for task %s",
			task.TaskIdentifier,
		)
	}
	if active {
		glog.V(3).Infof(
			"skip task %s: current_job %s still active (from frontmatter)",
			task.TaskIdentifier, currentJob,
		)
		metrics.TaskEventsTotal.WithLabelValues("skipped_active_job").Inc()
		return true, nil
	}
	// Grace window: suppress respawn while the agent's terminal-phase write propagates.
	// Treat missing or unparseable job_started_at as elapsed (preserves legacy-task behavior).
	jobStartedAt, parseErr := task.Frontmatter.JobStartedAt()
	if parseErr != nil {
		glog.Warningf(
			"task %s: failed to parse job_started_at: %v; treating grace period as elapsed",
			task.TaskIdentifier, parseErr,
		)
	} else if !jobStartedAt.IsZero() {
		elapsed := h.currentDateTime.Now().Time().Sub(jobStartedAt)
		if elapsed < defaultRespawnGracePeriod {
			glog.Infof(
				"event=respawn_grace_window task=%s current_job=%s elapsed=%.0fs",
				task.TaskIdentifier, currentJob, elapsed.Seconds(),
			)
			metrics.TaskEventsTotal.WithLabelValues("respawn_grace_window").Inc()
			return true, nil
		}
	}
	glog.V(2).Infof(
		"task %s: current_job %s no longer active, proceeding to spawn",
		task.TaskIdentifier, currentJob,
	)
	return false, nil
}

// spawnIfNeeded runs the active-job checks and spawns a K8s Job when appropriate.
func (h *taskEventHandler) spawnIfNeeded(
	ctx context.Context,
	task lib.Task,
	config *pkg.AgentConfiguration,
) error {
	// If current_job is set in frontmatter, a prior spawn notification was written
	// to the task file. Verify the job is still active; if not, proceed to spawn.
	if currentJob := task.Frontmatter.CurrentJob(); currentJob != "" {
		suppress, err := h.checkActiveCurrentJob(ctx, task, currentJob)
		if err != nil {
			return err
		}
		if suppress {
			return nil
		}
	}

	active, err := h.jobSpawner.IsJobActive(ctx, task.TaskIdentifier)
	if err != nil {
		metrics.TaskEventsTotal.WithLabelValues("error").Inc()
		return errors.Wrapf(ctx, err, "check active job for task %s", task.TaskIdentifier)
	}
	if active {
		glog.V(3).Infof("skip task %s: active job exists", task.TaskIdentifier)
		metrics.TaskEventsTotal.WithLabelValues("skipped_active_job").Inc()
		return nil
	}

	if task.Frontmatter.TriggerCount() >= task.Frontmatter.MaxTriggers() {
		glog.V(2).Infof("skip task %s: trigger_count %d >= max_triggers %d",
			task.TaskIdentifier,
			task.Frontmatter.TriggerCount(),
			task.Frontmatter.MaxTriggers(),
		)
		metrics.TaskEventsTotal.WithLabelValues("skipped_trigger_cap").Inc()
		return nil
	}

	if err := h.resultPublisher.PublishIncrementTriggerCount(ctx, task); err != nil {
		metrics.TaskEventsTotal.WithLabelValues("error").Inc()
		return errors.Wrapf(
			ctx,
			err,
			"publish increment trigger_count for task %s",
			task.TaskIdentifier,
		)
	}

	jobName, err := h.jobSpawner.SpawnJob(ctx, task, *config)
	if err != nil {
		metrics.TaskEventsTotal.WithLabelValues("error").Inc()
		return errors.Wrapf(ctx, err, "spawn job for task %s failed", task.TaskIdentifier)
	}

	h.taskStore.Store(task.TaskIdentifier, task)
	if err := h.resultPublisher.PublishSpawnNotification(ctx, task, jobName); err != nil {
		// Log but don't fail — job is already spawned, spawn notification is best-effort
		glog.Warningf("publish spawn notification for task %s failed (job %s still running): %v",
			task.TaskIdentifier, jobName, err)
	}

	glog.V(2).Infof(
		"spawned job for task %s (assignee=%s image=%s)",
		task.TaskIdentifier, task.Frontmatter.Assignee(), config.Image,
	)
	metrics.TaskEventsTotal.WithLabelValues("spawned").Inc()
	metrics.JobsSpawnedTotal.Inc()
	return nil
}
