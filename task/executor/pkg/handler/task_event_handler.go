// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handler

import (
	"context"
	"encoding/json"

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
	agentConfigs pkg.AgentConfigurations,
) TaskEventHandler {
	return &taskEventHandler{
		jobSpawner:   jobSpawner,
		branch:       branch,
		agentConfigs: agentConfigs,
	}
}

type taskEventHandler struct {
	jobSpawner   spawner.JobSpawner
	branch       base.Branch
	agentConfigs pkg.AgentConfigurations
}

func (h *taskEventHandler) ConsumeMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	if len(msg.Value) == 0 {
		glog.V(3).Infof("skip empty message at offset %d", msg.Offset)
		return nil
	}

	var task lib.Task
	if err := json.Unmarshal(msg.Value, &task); err != nil {
		glog.Warningf("failed to unmarshal task event at offset %d: %v", msg.Offset, err)
		return nil
	}

	if task.TaskIdentifier == "" {
		glog.Warningf("task event at offset %d has empty TaskIdentifier, skipping", msg.Offset)
		return nil
	}

	if task.Frontmatter.Status() != "in_progress" {
		glog.V(3).
			Infof("skip task %s with status %s", task.TaskIdentifier, task.Frontmatter.Status())
		metrics.TaskEventsTotal.WithLabelValues("skipped_status").Inc()
		return nil
	}

	phase := task.Frontmatter.Phase()
	if phase == nil || !allowedPhases.Contains(*phase) {
		glog.V(3).Infof("skip task %s with phase %v", task.TaskIdentifier, phase)
		metrics.TaskEventsTotal.WithLabelValues("skipped_phase").Inc()
		return nil
	}

	stage := task.Frontmatter.Stage()
	if stage != string(h.branch) {
		glog.V(3).Infof(
			"skip task %s with stage %s (executor branch %s)",
			task.TaskIdentifier, stage, h.branch,
		)
		metrics.TaskEventsTotal.WithLabelValues("skipped_stage").Inc()
		return nil
	}

	if task.Frontmatter.Assignee() == "" {
		glog.V(3).Infof("skip task %s with empty assignee", task.TaskIdentifier)
		metrics.TaskEventsTotal.WithLabelValues("skipped_assignee").Inc()
		return nil
	}

	config, ok := h.agentConfigs.FindByAssignee(string(task.Frontmatter.Assignee()))
	if !ok {
		glog.Warningf(
			"skip task %s: unknown assignee %s",
			task.TaskIdentifier,
			task.Frontmatter.Assignee(),
		)
		metrics.TaskEventsTotal.WithLabelValues("skipped_unknown_assignee").Inc()
		return nil
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

	if err := h.jobSpawner.SpawnJob(ctx, task, config); err != nil {
		metrics.TaskEventsTotal.WithLabelValues("error").Inc()
		return errors.Wrapf(ctx, err, "spawn job for task %s failed", task.TaskIdentifier)
	}

	glog.V(2).
		Infof("spawned job for task %s (assignee=%s image=%s)", task.TaskIdentifier, task.Frontmatter.Assignee(), config.Image)
	metrics.TaskEventsTotal.WithLabelValues("spawned").Inc()
	metrics.JobsSpawnedTotal.Inc()
	return nil
}
