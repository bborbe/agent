// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"

	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	"github.com/bborbe/vault-cli/pkg/domain"
	"github.com/golang/glog"

	lib "github.com/bborbe/agent/lib"
	pkg "github.com/bborbe/agent/task/executor/pkg"
	"github.com/bborbe/agent/task/executor/pkg/metrics"
	"github.com/bborbe/agent/task/executor/pkg/spawner"
)

// allowedPhases lists the task phases that qualify for job spawning.
var allowedPhases = domain.TaskPhases{
	domain.TaskPhasePlanning,
	domain.TaskPhaseInProgress,
	domain.TaskPhaseAIReview,
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
) TaskEventHandler {
	return &taskEventHandler{
		jobSpawner:      jobSpawner,
		branch:          branch,
		resolver:        resolver,
		resultPublisher: resultPublisher,
		taskStore:       taskStore,
	}
}

type taskEventHandler struct {
	jobSpawner      spawner.JobSpawner
	branch          base.Branch
	resolver        pkg.ConfigResolver
	resultPublisher pkg.ResultPublisher
	taskStore       *pkg.TaskStore
}

func (h *taskEventHandler) ConsumeMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	task, skip := h.parseAndFilter(msg)
	if skip {
		return nil
	}
	return h.spawnIfNeeded(ctx, task)
}

// parseAndFilter unmarshals the message and applies all pre-spawn filter checks.
// Returns (task, true) when the message should be skipped.
// Returns (task, false) when the task qualifies for spawning.
func (h *taskEventHandler) parseAndFilter(msg *sarama.ConsumerMessage) (lib.Task, bool) {
	if len(msg.Value) == 0 {
		glog.V(3).Infof("skip empty message at offset %d", msg.Offset)
		return lib.Task{}, true
	}

	var task lib.Task
	if err := json.Unmarshal(msg.Value, &task); err != nil {
		glog.Warningf("failed to unmarshal task event at offset %d: %v", msg.Offset, err)
		return lib.Task{}, true
	}

	if task.TaskIdentifier == "" {
		glog.Warningf("task event at offset %d has empty TaskIdentifier, skipping", msg.Offset)
		return lib.Task{}, true
	}

	// Clean up taskStore for completed tasks so the job informer does not emit
	// a spurious synthetic failure after the agent has already published success.
	if string(task.Frontmatter.Status()) == "completed" {
		h.taskStore.Delete(task.TaskIdentifier)
		glog.V(3).Infof("task %s completed: removed from task store", task.TaskIdentifier)
	}

	if task.Frontmatter.Status() != "in_progress" {
		glog.V(3).
			Infof("skip task %s with status %s", task.TaskIdentifier, task.Frontmatter.Status())
		metrics.TaskEventsTotal.WithLabelValues("skipped_status").Inc()
		return lib.Task{}, true
	}

	phase := task.Frontmatter.Phase()
	if phase == nil || !allowedPhases.Contains(*phase) {
		glog.V(3).Infof("skip task %s with phase %v", task.TaskIdentifier, phase)
		metrics.TaskEventsTotal.WithLabelValues("skipped_phase").Inc()
		return lib.Task{}, true
	}

	stage := task.Frontmatter.Stage()
	if stage != string(h.branch) {
		glog.V(3).Infof(
			"skip task %s with stage %s (executor branch %s)",
			task.TaskIdentifier, stage, h.branch,
		)
		metrics.TaskEventsTotal.WithLabelValues("skipped_stage").Inc()
		return lib.Task{}, true
	}

	if task.Frontmatter.Assignee() == "" {
		glog.V(3).Infof("skip task %s with empty assignee", task.TaskIdentifier)
		metrics.TaskEventsTotal.WithLabelValues("skipped_assignee").Inc()
		return lib.Task{}, true
	}

	return task, false
}

// spawnIfNeeded runs the active-job checks and spawns a K8s Job when appropriate.
func (h *taskEventHandler) spawnIfNeeded(ctx context.Context, task lib.Task) error {
	config, err := h.resolveConfig(ctx, task)
	if err != nil {
		return err
	}
	if config == nil {
		return nil
	}

	// If current_job is set in frontmatter, a prior spawn notification was written
	// to the task file. Verify the job is still active; if not, proceed to spawn.
	if currentJob := task.Frontmatter.CurrentJob(); currentJob != "" {
		active, err := h.jobSpawner.IsJobActive(ctx, task.TaskIdentifier)
		if err != nil {
			metrics.TaskEventsTotal.WithLabelValues("error").Inc()
			return errors.Wrapf(
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
			return nil
		}
		glog.V(2).Infof(
			"task %s: current_job %s no longer active, proceeding to spawn",
			task.TaskIdentifier, currentJob,
		)
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

// resolveConfig looks up the agent configuration for the task's assignee.
// Returns (nil, nil) when the task should be silently skipped (unknown assignee).
// Returns (nil, err) for unexpected resolver failures.
func (h *taskEventHandler) resolveConfig(
	ctx context.Context,
	task lib.Task,
) (*pkg.AgentConfiguration, error) {
	config, err := h.resolver.Resolve(ctx, string(task.Frontmatter.Assignee()))
	if err != nil {
		if stderrors.Is(err, pkg.ErrConfigNotFound) {
			glog.Warningf(
				"skip task %s: unknown assignee %s",
				task.TaskIdentifier,
				task.Frontmatter.Assignee(),
			)
			metrics.TaskEventsTotal.WithLabelValues("skipped_unknown_assignee").Inc()
			return nil, nil
		}
		metrics.TaskEventsTotal.WithLabelValues("error").Inc()
		return nil, errors.Wrapf(ctx, err, "resolve agent config for task %s", task.TaskIdentifier)
	}
	return &config, nil
}
