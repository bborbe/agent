// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handler

import (
	"context"
	"encoding/json"

	"github.com/IBM/sarama"
	"github.com/bborbe/errors"
	"github.com/bborbe/vault-cli/pkg/domain"
	"github.com/golang/glog"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/executor/pkg/spawner"
)

// allowedPhases lists the task phases that qualify for job spawning.
var allowedPhases = domain.TaskPhases{
	domain.TaskPhasePlanning,
	domain.TaskPhaseInProgress,
	domain.TaskPhaseAIReview,
}

//counterfeiter:generate -o ../../mocks/duplicate_tracker.go --fake-name FakeDuplicateTracker . DuplicateTracker

// DuplicateTracker tracks which TaskIdentifiers have already spawned a Job in this process lifetime.
type DuplicateTracker interface {
	// IsDuplicate returns true if the given TaskIdentifier was already processed.
	IsDuplicate(id lib.TaskIdentifier) bool
	// MarkProcessed records the TaskIdentifier so future calls to IsDuplicate return true.
	MarkProcessed(id lib.TaskIdentifier)
}

// NewInMemoryDuplicateTracker creates a new in-memory DuplicateTracker.
func NewInMemoryDuplicateTracker() DuplicateTracker {
	return &inMemoryDuplicateTracker{
		seen: make(map[lib.TaskIdentifier]struct{}),
	}
}

type inMemoryDuplicateTracker struct {
	seen map[lib.TaskIdentifier]struct{}
}

func (t *inMemoryDuplicateTracker) IsDuplicate(id lib.TaskIdentifier) bool {
	_, ok := t.seen[id]
	return ok
}

func (t *inMemoryDuplicateTracker) MarkProcessed(id lib.TaskIdentifier) {
	t.seen[id] = struct{}{}
}

//counterfeiter:generate -o ../../mocks/task_event_handler.go --fake-name FakeTaskEventHandler . TaskEventHandler

// TaskEventHandler processes a single task event message from Kafka.
type TaskEventHandler interface {
	ConsumeMessage(ctx context.Context, msg *sarama.ConsumerMessage) error
}

// NewTaskEventHandler creates a new TaskEventHandler.
func NewTaskEventHandler(
	duplicateTracker DuplicateTracker,
	jobSpawner spawner.JobSpawner,
	assigneeImages map[string]string,
) TaskEventHandler {
	return &taskEventHandler{
		duplicateTracker: duplicateTracker,
		jobSpawner:       jobSpawner,
		assigneeImages:   assigneeImages,
	}
}

type taskEventHandler struct {
	duplicateTracker DuplicateTracker
	jobSpawner       spawner.JobSpawner
	assigneeImages   map[string]string
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

	if task.Status != "in_progress" {
		glog.V(3).Infof("skip task %s with status %s", task.TaskIdentifier, task.Status)
		return nil
	}

	if task.Phase == nil || !allowedPhases.Contains(*task.Phase) {
		glog.V(3).Infof("skip task %s with phase %v", task.TaskIdentifier, task.Phase)
		return nil
	}

	if task.Assignee == "" {
		glog.V(3).Infof("skip task %s with empty assignee", task.TaskIdentifier)
		return nil
	}

	image, ok := h.assigneeImages[string(task.Assignee)]
	if !ok {
		glog.Warningf("skip task %s: unknown assignee %s", task.TaskIdentifier, task.Assignee)
		return nil
	}

	if h.duplicateTracker.IsDuplicate(task.TaskIdentifier) {
		glog.V(3).Infof("skip duplicate task %s", task.TaskIdentifier)
		return nil
	}

	if err := h.jobSpawner.SpawnJob(ctx, task, image); err != nil {
		return errors.Wrapf(ctx, err, "spawn job for task %s failed", task.TaskIdentifier)
	}

	h.duplicateTracker.MarkProcessed(task.TaskIdentifier)
	glog.V(2).Infof("spawned job for task %s (assignee=%s)", task.TaskIdentifier, task.Assignee)
	return nil
}
