// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handler

import (
	"context"
	"encoding/json"

	"github.com/IBM/sarama"
	"github.com/bborbe/errors"
	"github.com/golang/glog"
	"github.com/google/uuid"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/prompt/controller/pkg/publisher"
)

//counterfeiter:generate -o ../../mocks/duplicate_tracker.go --fake-name FakeDuplicateTracker . DuplicateTracker

// DuplicateTracker tracks which TaskIdentifiers have already produced a prompt in this process lifetime.
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
	promptPublisher publisher.PromptPublisher,
) TaskEventHandler {
	return &taskEventHandler{
		duplicateTracker: duplicateTracker,
		promptPublisher:  promptPublisher,
	}
}

type taskEventHandler struct {
	duplicateTracker DuplicateTracker
	promptPublisher  publisher.PromptPublisher
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

	if task.Assignee == "" {
		glog.V(3).Infof("skip task %s with empty assignee", task.TaskIdentifier)
		return nil
	}

	if h.duplicateTracker.IsDuplicate(task.TaskIdentifier) {
		glog.V(3).Infof("skip duplicate task %s", task.TaskIdentifier)
		return nil
	}

	prompt := lib.Prompt{
		PromptIdentifier: lib.PromptIdentifier(uuid.New().String()),
		TaskIdentifier:   task.TaskIdentifier,
		Assignee:         task.Assignee,
		Instruction:      lib.PromptInstruction(task.Content),
	}

	if err := h.promptPublisher.PublishPrompt(ctx, prompt); err != nil {
		return errors.Wrapf(ctx, err, "publish prompt for task %s failed", task.TaskIdentifier)
	}

	h.duplicateTracker.MarkProcessed(task.TaskIdentifier)
	glog.V(2).Infof("published prompt %s for task %s", prompt.PromptIdentifier, task.TaskIdentifier)
	return nil
}
