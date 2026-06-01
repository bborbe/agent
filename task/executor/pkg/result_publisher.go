// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bborbe/cqrs/base"
	cdb "github.com/bborbe/cqrs/cdb"
	cqrsiam "github.com/bborbe/cqrs/iam"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"
	libtime "github.com/bborbe/time"
	"github.com/golang/glog"

	lib "github.com/bborbe/agent/lib"
	taskcmd "github.com/bborbe/agent/lib/command/task"
)

const dedupeCapacity = 1024

const dedupeTTL = 3600 * time.Second

//counterfeiter:generate -o ../mocks/result_publisher.go --fake-name FakeResultPublisher . ResultPublisher

// ResultPublisher publishes agent-task-v1-request commands to Kafka so the
// controller writes them to the vault task file.
type ResultPublisher interface {
	// PublishSpawnNotification publishes current_job, job_started_at, and
	// spawn_notification without touching any other frontmatter keys.
	PublishSpawnNotification(ctx context.Context, task lib.Task, jobName string) error
	// PublishFailure publishes a zombie failure: clears current_job and atomically
	// bumps trigger_count by 1 via a paired IncrementFrontmatterCommand. Leaves
	// phase, status, and assignee untouched so the existing trigger_count retry
	// cap (applyTriggerCap in task/controller/pkg/result/result_writer.go) handles
	// eventual operator-inbox escalation. Idempotent per current_job via a TTL'd
	// LRU; concurrent classifications for the same job emit one event.
	PublishFailure(ctx context.Context, task lib.Task, jobName string, reason string) error
	// PublishIncrementTriggerCount sends an IncrementFrontmatterCommand that atomically
	// increments trigger_count by 1. Must complete before SpawnJob is called.
	PublishIncrementTriggerCount(ctx context.Context, task lib.Task) error
	// PublishTypeMismatchFailure publishes a synthetic failure when the task's task_type
	// is not in the agent's effective type set. Clears assignee and current_job so the
	// task surfaces in the operator inbox via assignee=="" filter. Does not bump
	// trigger_count or retry_count.
	PublishTypeMismatchFailure(ctx context.Context, task lib.Task, reason string) error
	// PublishRaw publishes a raw payload for testing error paths.
	PublishRaw(ctx context.Context, operation base.CommandOperation, payload interface{}) error
}

// NewResultPublisher creates a ResultPublisher.
func NewResultPublisher(
	syncProducer libkafka.SyncProducer,
	branch base.Branch,
	currentDateTime libtime.CurrentDateTimeGetter,
) ResultPublisher {
	return &resultPublisher{
		commandObjectSender: cdb.NewCommandObjectSender(
			syncProducer,
			branch,
			log.DefaultSamplerFactory,
		),
		currentDateTime: currentDateTime,
		dedupe:          newDedupe(dedupeCapacity),
	}
}

// dedupeEntry tracks a job name and its insertion timestamp for TTL-based LRU eviction.
type dedupeEntry struct {
	jobName string
	ts      time.Time
}

// ttlDedupe implements a minimal TTL'd LRU with RWMutex for publish-layer dedupe.
type ttlDedupe struct {
	mu       sync.RWMutex
	capacity int
	ttl      time.Duration
	entries  []dedupeEntry // insertion-order list; oldest at index 0
	index    map[string]int
}

func newDedupe(capacity int) *ttlDedupe {
	return &ttlDedupe{
		capacity: capacity,
		ttl:      dedupeTTL,
		entries:  make([]dedupeEntry, 0, capacity),
		index:    make(map[string]int),
	}
}

// checkDedupe returns true if a non-expired entry exists for jobName.
// No mutation occurs.
func (d *ttlDedupe) checkDedupe(jobName string, now time.Time) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if idx, ok := d.index[jobName]; ok {
		if now.Sub(d.entries[idx].ts) < d.ttl {
			return true
		}
	}
	return false
}

// recordDedupe inserts or refreshes the entry for jobName with the current timestamp.
// Evicts oldest entries if at capacity.
func (d *ttlDedupe) recordDedupe(jobName string, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if idx, ok := d.index[jobName]; ok {
		d.entries[idx].ts = now
		return
	}
	if len(d.entries) >= d.capacity {
		evicted := d.entries[0]
		delete(d.index, evicted.jobName)
		d.entries = d.entries[1:]
	}
	d.entries = append(d.entries, dedupeEntry{jobName: jobName, ts: now})
	d.index[jobName] = len(d.entries) - 1
}

// resultPublisher implements ResultPublisher by sending CQRS command objects to Kafka.
type resultPublisher struct {
	commandObjectSender cdb.CommandObjectSender
	currentDateTime     libtime.CurrentDateTimeGetter
	dedupe              *ttlDedupe
}

func (p *resultPublisher) PublishSpawnNotification(
	ctx context.Context,
	task lib.Task,
	jobName string,
) error {
	cmd := taskcmd.UpdateFrontmatterCommand{
		TaskIdentifier: task.TaskIdentifier,
		Updates: lib.TaskFrontmatter{
			"spawn_notification": true,
			"current_job":        jobName,
			"job_started_at":     p.currentDateTime.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
		},
	}
	return p.publishRaw(ctx, taskcmd.UpdateFrontmatterCommandOperation, cmd)
}

func (p *resultPublisher) PublishFailure(
	ctx context.Context,
	task lib.Task,
	jobName string,
	reason string,
) error {
	nowTs := p.currentDateTime.Now()
	if p.dedupe.checkDedupe(jobName, nowTs.Time()) {
		glog.V(2).Infof("event=zombie_dedupe job=%s task=%s", jobName, task.TaskIdentifier)
		return nil
	}

	now := nowTs.UTC().Format(time.RFC3339)
	section := fmt.Sprintf(
		"## Failure\n\n- **Timestamp:** %s\n- **Job:** %s\n- **Reason:** %s\n",
		now,
		jobName,
		reason,
	)
	updateCmd := taskcmd.UpdateFrontmatterCommand{
		TaskIdentifier: task.TaskIdentifier,
		Updates: lib.TaskFrontmatter{
			"current_job": "",
		},
		Body: &taskcmd.BodySection{
			Heading: "## Failure",
			Section: section,
		},
	}
	if err := p.publishRaw(ctx, taskcmd.UpdateFrontmatterCommandOperation, updateCmd); err != nil {
		return errors.Wrapf(
			ctx,
			err,
			"publish zombie failure update for task %s",
			task.TaskIdentifier,
		)
	}

	incrementCmd := taskcmd.IncrementFrontmatterCommand{
		TaskIdentifier: task.TaskIdentifier,
		Field:          "trigger_count",
		Delta:          1,
	}
	if err := p.publishRaw(ctx, taskcmd.IncrementFrontmatterCommandOperation, incrementCmd); err != nil {
		return errors.Wrapf(
			ctx,
			err,
			"publish zombie failure trigger_count increment for task %s",
			task.TaskIdentifier,
		)
	}

	p.dedupe.recordDedupe(jobName, nowTs.Time())
	return nil
}

func (p *resultPublisher) PublishIncrementTriggerCount(ctx context.Context, task lib.Task) error {
	cmd := taskcmd.IncrementFrontmatterCommand{
		TaskIdentifier: task.TaskIdentifier,
		Field:          "trigger_count",
		Delta:          1,
	}
	return p.publishRaw(ctx, taskcmd.IncrementFrontmatterCommandOperation, cmd)
}

func (p *resultPublisher) PublishTypeMismatchFailure(
	ctx context.Context,
	task lib.Task,
	reason string,
) error {
	now := p.currentDateTime.Now().UTC().Format(time.RFC3339)
	priorAssignee := string(task.Frontmatter.Assignee())
	section := fmt.Sprintf(
		"## Failure\n\n- **Timestamp:** %s\n- **Assignee:** %s\n- **Reason:** %s\n",
		now,
		priorAssignee,
		reason,
	)

	updates := lib.TaskFrontmatter{
		"assignee":    "",
		"current_job": "",
	}
	if priorAssignee != "" {
		updates["previous_assignee"] = priorAssignee
	}

	cmd := taskcmd.UpdateFrontmatterCommand{
		TaskIdentifier: task.TaskIdentifier,
		Updates:        updates,
		Body: &taskcmd.BodySection{
			Heading: "## Failure",
			Section: section,
		},
	}
	if err := p.publishRaw(ctx, taskcmd.UpdateFrontmatterCommandOperation, cmd); err != nil {
		return errors.Wrapf(
			ctx,
			err,
			"publish type mismatch failure for task %s",
			task.TaskIdentifier,
		)
	}
	return nil
}

func (p *resultPublisher) publishRaw(
	ctx context.Context,
	operation base.CommandOperation,
	payload interface{},
) error {
	event, err := base.ParseEvent(ctx, payload)
	if err != nil {
		return errors.Wrapf(ctx, err, "parse event for operation %s", operation)
	}

	requestIDCh := make(chan base.RequestID, 1)
	requestIDCh <- base.NewRequestID()
	commandCreator := base.NewCommandCreator(requestIDCh)
	commandObject := cdb.CommandObject{
		Command: commandCreator.NewCommand(
			operation,
			cqrsiam.Initiator("executor"),
			"",
			event,
		),
		SchemaID: lib.TaskV1SchemaID,
	}
	if err := p.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
		return errors.Wrapf(ctx, err, "send command for operation %s", operation)
	}
	return nil
}

// PublishRaw exposes publishRaw for testing error path coverage.
func (p *resultPublisher) PublishRaw(
	ctx context.Context,
	operation base.CommandOperation,
	payload interface{},
) error {
	return p.publishRaw(ctx, operation, payload)
}
