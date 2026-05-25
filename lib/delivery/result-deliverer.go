// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery

import (
	"context"
	"os"

	"github.com/bborbe/cqrs/base"
	cdb "github.com/bborbe/cqrs/cdb"
	cqrsiam "github.com/bborbe/cqrs/iam"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"
	libtime "github.com/bborbe/time"
	domain "github.com/bborbe/vault-cli/pkg/domain"
	"github.com/golang/glog"
	"github.com/google/uuid"

	agentlib "github.com/bborbe/agent/lib"
)

// NewNoopResultDeliverer creates a agentlib.ResultDeliverer that does nothing.
func NewNoopResultDeliverer() agentlib.ResultDeliverer {
	return &noopResultDeliverer{}
}

type noopResultDeliverer struct{}

func (n *noopResultDeliverer) DeliverResult(_ context.Context, _ agentlib.AgentResultInfo) error {
	return nil
}

// NewFileResultDeliverer creates a agentlib.ResultDeliverer that writes results to a task file.
// The generator produces the complete updated markdown; the deliverer writes it to disk.
func NewFileResultDeliverer(generator ContentGenerator, filePath string) agentlib.ResultDeliverer {
	return &fileResultDeliverer{generator: generator, filePath: filePath}
}

type fileResultDeliverer struct {
	generator ContentGenerator
	filePath  string
}

func (d *fileResultDeliverer) DeliverResult(
	ctx context.Context,
	result agentlib.AgentResultInfo,
) error {
	original, err := os.ReadFile(
		d.filePath,
	) // #nosec G304 -- filePath validated by caller
	if err != nil {
		return errors.Wrap(ctx, err, "read task file failed")
	}

	generated, err := d.generator.Generate(ctx, string(original), result)
	if err != nil {
		return errors.Wrap(ctx, err, "content generation failed")
	}

	if err := os.WriteFile(d.filePath, []byte(generated), 0600); err != nil { // #nosec G304 G703 -- filePath validated by caller
		return errors.Wrap(ctx, err, "write task file failed")
	}
	return nil
}

// NewKafkaResultDeliverer creates a agentlib.ResultDeliverer that publishes task updates to Kafka.
// taskID must be non-empty; if empty, use NewNoopResultDeliverer instead.
// originalContent is the original task markdown; the generator produces the complete updated content.
func NewKafkaResultDeliverer(
	syncProducer libkafka.SyncProducer,
	branch base.Branch,
	taskID agentlib.TaskIdentifier,
	originalContent string,
	generator ContentGenerator,
	currentDateTime libtime.CurrentDateTimeGetter,
) agentlib.ResultDeliverer {
	return NewKafkaResultDelivererWithSender(
		cdb.NewCommandObjectSender(syncProducer, branch, log.DefaultSamplerFactory),
		taskID,
		originalContent,
		generator,
		currentDateTime,
	)
}

// NewKafkaResultDelivererWithSender creates a agentlib.ResultDeliverer that publishes task updates via the given sender.
// This constructor is primarily useful for testing.
func NewKafkaResultDelivererWithSender(
	commandObjectSender cdb.CommandObjectSender,
	taskID agentlib.TaskIdentifier,
	originalContent string,
	generator ContentGenerator,
	currentDateTime libtime.CurrentDateTimeGetter,
) agentlib.ResultDeliverer {
	return &kafkaResultDeliverer{
		commandObjectSender: commandObjectSender,
		taskID:              taskID,
		originalContent:     originalContent,
		generator:           generator,
		currentDateTime:     currentDateTime,
	}
}

type kafkaResultDeliverer struct {
	commandObjectSender cdb.CommandObjectSender
	taskID              agentlib.TaskIdentifier
	originalContent     string
	generator           ContentGenerator
	currentDateTime     libtime.CurrentDateTimeGetter
}

func (d *kafkaResultDeliverer) DeliverResult(
	ctx context.Context,
	result agentlib.AgentResultInfo,
) error {
	generated, err := d.generator.Generate(ctx, d.originalContent, result)
	if err != nil {
		return errors.Wrap(ctx, err, "content generation failed")
	}

	fmMap, body := ParseMarkdownFrontmatter(generated)

	frontmatter := agentlib.TaskFrontmatter{}
	for k, v := range fmMap {
		frontmatter[k] = v
	}

	// Set status/assignee from result.Status directly. The content generator may not
	// have frontmatter to update (TASK_CONTENT is body-only), so we set it explicitly.
	// Failed and needs_input results clear assignee (operator inbox surfaces empty-assignee
	// tasks) and leave phase unchanged. Only the AgentStatusDone branch may write
	// phase: human_review, and only when the agent itself requested it via Result.NextPhase.
	// Retry of failed tasks is the controller's responsibility via trigger_count /
	// max_triggers, not a phase loop.
	switch result.Status {
	case agentlib.AgentStatusDone:
		resolvedPhase := resolveNextPhase(d.taskID, result.NextPhase)
		frontmatter["phase"] = resolvedPhase
		// Only mark the task completed when the resolved phase is terminal (done).
		// Requested transitions to planning/in_progress/ai_review/human_review keep
		// the task at status: in_progress so the controller re-triggers on the
		// new phase. Without this, multi-phase agents stall after their first phase.
		if resolvedPhase == "done" {
			frontmatter["status"] = "completed"
		} else {
			frontmatter["status"] = "in_progress"
		}
	case agentlib.AgentStatusNeedsInput:
		frontmatter["status"] = "in_progress"
		frontmatter["assignee"] = ""
		// phase is preserved from incoming frontmatter (already copied from fmMap above)
	case agentlib.AgentStatusInProgress:
		// Step-level progress save: keep status: in_progress, preserve phase from incoming
		// task frontmatter (already copied from fmMap above). NextPhase ignored on this status —
		// log a warning if the agent set both.
		if result.NextPhase != "" {
			glog.Warningf("task %s: ignoring NextPhase %q on Status: in_progress (in-place save)",
				d.taskID, result.NextPhase)
		}
		frontmatter["status"] = "in_progress"
		// phase intentionally not modified — preserves incoming phase
	default:
		frontmatter["status"] = "in_progress"
		frontmatter["assignee"] = ""
		// phase is preserved from incoming frontmatter (already copied from fmMap above)
	}

	now := d.currentDateTime.Now()
	task := agentlib.Task{
		Object: base.Object[base.Identifier]{
			Identifier: base.Identifier(uuid.New().String()),
			Created:    now,
			Modified:   now,
		},
		TaskIdentifier: d.taskID,
		Frontmatter:    frontmatter,
		Content:        agentlib.TaskContent(body),
	}

	event, err := base.ParseEvent(ctx, task)
	if err != nil {
		return errors.Wrap(ctx, err, "parse task event failed")
	}

	requestIDCh := make(chan base.RequestID, 1)
	requestIDCh <- base.NewRequestID()
	commandCreator := base.NewCommandCreator(requestIDCh)
	commandObject := cdb.CommandObject{
		Command: commandCreator.NewCommand(
			base.UpdateOperation,
			cqrsiam.Initiator("agent"),
			"",
			event,
		),
		SchemaID: agentlib.TaskV1SchemaID,
	}

	glog.V(2).Infof("publishing task update for taskID=%s status=%s", d.taskID, result.Status)
	if err := d.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
		return errors.Wrap(ctx, err, "publish task update failed")
	}
	return nil
}

// resolveNextPhase returns the validated phase string for a done agent result.
// An empty NextPhase defaults to "done" (existing behavior). An invalid value
// is logged with task-id context and also falls back to "done" — we never refuse
// to write a result just because the agent requested a bogus phase.
func resolveNextPhase(
	taskID agentlib.TaskIdentifier,
	requested string,
) string {
	if requested == "" {
		return "done"
	}
	canonical, ok := domain.NormalizeTaskPhase(requested)
	if !ok {
		glog.Warningf("task %s: ignoring invalid NextPhase %q: unknown phase", taskID, requested)
		return "done"
	}
	return string(canonical)
}
