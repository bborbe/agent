// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery

import (
	"context"
	"os"
	"sync"

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

	agentlib "github.com/bborbe/agent"
)

// metricsAgentTurnsKey and metricsInteractionCountKey are the frozen frontmatter keys
// this deliverer publishes for a Claude-backed run (spec 053). Lowercase, snake_case,
// no nesting, no version suffix, no alias.
const (
	metricsAgentTurnsKey       = "metrics_agent_turns"
	metricsInteractionCountKey = "metrics_interaction_count"
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

	// mu serialises DeliverResult so advancedPhase is read and written safely.
	mu sync.Mutex
	// advancedPhase is the last non-terminal phase this deliverer wrote on a
	// Done + NextPhase result.
	advancedPhase string
}

func (d *fileResultDeliverer) DeliverResult(
	ctx context.Context,
	result agentlib.AgentResultInfo,
) error {
	d.mu.Lock()
	defer d.mu.Unlock()

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

	generated = d.carryAdvancedPhase(generated, result)

	generated = d.preserveDiskPhase(generated, string(original), result)

	if err := os.WriteFile(d.filePath, []byte(generated), 0600); err != nil { // #nosec G304 G703 -- filePath validated by caller
		return errors.Wrap(ctx, err, "write task file failed")
	}
	return nil
}

// carryAdvancedPhase keeps an in-process phase advance from being clobbered by
// a later save in the same Job, operating on the generated markdown. Caller must
// hold d.mu.
func (d *fileResultDeliverer) carryAdvancedPhase(
	generated string,
	result agentlib.AgentResultInfo,
) string {
	if result.Status == agentlib.AgentStatusDone && result.NextPhase != "" {
		fm, _ := ParseMarkdownFrontmatter(generated)
		if phase, _ := fm["phase"].(string); phase != "" && phase != "done" {
			d.advancedPhase = phase
		}
		return generated
	}
	if d.advancedPhase == "" {
		return generated
	}
	return SetFrontmatterField(generated, "phase", d.advancedPhase)
}

// preserveDiskPhase keeps a result that advances no phase from overwriting the
// phase currently on disk with the Job-start snapshot the generator rebuilds
// from. A whole-file write cannot omit the key the way the Kafka payload can, so
// the on-disk value is carried forward instead. Anything unreadable falls back
// to the generated content — a delivery is never failed over this.
func (d *fileResultDeliverer) preserveDiskPhase(
	generated string,
	original string,
	result agentlib.AgentResultInfo,
) string {
	if result.Status == agentlib.AgentStatusDone && result.NextPhase != "" {
		return generated
	}
	fm, _ := ParseMarkdownFrontmatter(original)
	phase, _ := fm["phase"].(string)
	if phase == "" {
		return generated
	}
	return SetFrontmatterField(generated, "phase", phase)
}

// NewKafkaResultDeliverer creates a agentlib.ResultDeliverer that publishes task updates to Kafka.
// taskID must be non-empty; if empty, use NewNoopResultDeliverer instead.
// originalContent is the original task markdown; the generator produces the complete updated content.
// topicPrefix selects the Kafka topic prefix (e.g. base.TopicPrefixFromBranch(branch) to
// preserve the legacy dev/prod topic names, or "" for unprefixed topics).
func NewKafkaResultDeliverer(
	syncProducer libkafka.SyncProducer,
	topicPrefix base.TopicPrefix,
	taskID agentlib.TaskIdentifier,
	originalContent string,
	generator ContentGenerator,
	currentDateTime libtime.CurrentDateTimeGetter,
) agentlib.ResultDeliverer {
	return NewKafkaResultDelivererWithSender(
		cdb.NewCommandObjectSender(syncProducer, topicPrefix, log.DefaultSamplerFactory),
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

	// mu serialises DeliverResult so advancedPhase is read and written safely.
	mu sync.Mutex
	// advancedPhase is the last non-terminal phase this deliverer published on a
	// Done + NextPhase result. A deliverer is constructed per Job (with that Job's
	// originalContent), so this is per-Job state: Agent.Run advances phases
	// in-process with one deliverer, and the failed branch — the only branch that
	// still publishes a phase — must publish this phase rather than the Job-start
	// phase it was built from. Empty means "no advance yet", in which case the
	// generated (Job-start) phase is the best available answer.
	advancedPhase string
}

func (d *kafkaResultDeliverer) DeliverResult(
	ctx context.Context,
	result agentlib.AgentResultInfo,
) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	generated, err := d.generator.Generate(ctx, d.originalContent, result)
	if err != nil {
		return errors.Wrap(ctx, err, "content generation failed")
	}

	fmMap, body := ParseMarkdownFrontmatter(generated)

	frontmatter := agentlib.TaskFrontmatter{}
	for k, v := range fmMap {
		frontmatter[k] = v
	}

	d.applyResultMetrics(frontmatter, result)

	d.applyResultFrontmatter(frontmatter, result)

	d.stampTargetVault(frontmatter)

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

// applyResultFrontmatter merges result.Status into the task frontmatter — status,
// assignee, and phase — per the retry-vs-escalate contract. Extracted into a helper
// so DeliverResult stays under the funlen limit.
//
// Retry-vs-escalate (spec 010/021): failed is an infra failure (transient — crash,
// timeout, OOM), so preserve assignee and keep the task routable. The retry is
// bounded by retry_count / max_retries (see retry-bound.go), with the pre-existing
// trigger_count / max_triggers cap kept as a second stop so no behaviour regresses.
// Only at bound exhaustion does the task escalate: assignee cleared to "" (the
// operator-inbox signal), previous_assignee recorded (spec 027), phase left as the
// resume cursor. Any forward-progress result clears the retry streak.
//
// needs_input is task-wrong, not transient — the agent already did the work, so the
// task surfaces in the operator inbox immediately (assignee cleared, no phase published).
// Only the AgentStatusDone branch may write phase: human_review, and only when the
// agent itself requested it via Result.NextPhase.
func (d *kafkaResultDeliverer) applyResultFrontmatter(
	frontmatter agentlib.TaskFrontmatter,
	result agentlib.AgentResultInfo,
) {
	switch result.Status {
	case agentlib.AgentStatusDone:
		if result.NextPhase == "" {
			// Done without NextPhase is an in-place save (see agentlib.Result.NextPhase:
			// "Empty means stay in current phase"): keep status: in_progress and publish
			// no phase at all. Terminating a task requires an explicit NextPhase: "done".
			frontmatter["status"] = "in_progress"
			resetRetryCount(frontmatter)
			// A completed run does not own the phase. Omitting the key is a preserve,
			// not a delete: the controller merges on-disk-first (MergeFrontmatter), so
			// the task keeps whatever phase it currently holds — including one written
			// by a park while this run was still going.
			delete(frontmatter, "phase")
			break
		}
		resolvedPhase := resolveNextPhase(d.taskID, result.NextPhase)
		frontmatter["phase"] = resolvedPhase
		// Remember the advance for the rest of this Job. Every other branch now
		// publishes no phase, so the on-disk value survives the controller's merge —
		// but the failed branch deliberately still publishes one, and it must publish
		// THIS phase rather than the Job-start phase the generator rebuilt from.
		if resolvedPhase != "done" {
			d.advancedPhase = resolvedPhase
		}
		// Only mark the task completed when the resolved phase is terminal (done).
		// Requested transitions to planning/execution/ai_review/human_review keep
		// the task at status: in_progress so the controller re-triggers on the
		// new phase. Without this, multi-phase agents stall after their first phase.
		if resolvedPhase == "done" {
			frontmatter["status"] = "completed"
		} else {
			frontmatter["status"] = "in_progress"
		}
		resetRetryCount(frontmatter)
	case agentlib.AgentStatusNeedsInput:
		frontmatter["status"] = "in_progress"
		// Task-wrong (permanent): the agent already did the work, so the task surfaces
		// in the operator inbox immediately. Record the pre-clear owner (spec 027).
		if prev := string(frontmatter.Assignee()); prev != "" {
			frontmatter["previous_assignee"] = prev
		}
		frontmatter["assignee"] = ""
		// This result advances nothing: omit the phase so the controller's merge keeps
		// the on-disk value rather than republishing the Job-start snapshot.
		delete(frontmatter, "phase")
	case agentlib.AgentStatusInProgress:
		// Step-level progress save: keep status: in_progress, publish no phase so the
		// controller's merge keeps the on-disk one. NextPhase ignored on this status.
		if result.NextPhase != "" {
			glog.Warningf("task %s: ignoring NextPhase %q on Status: in_progress (in-place save)",
				d.taskID, result.NextPhase)
		}
		frontmatter["status"] = "in_progress"
		resetRetryCount(frontmatter)
		delete(frontmatter, "phase")
	case agentlib.AgentStatusFailed:
		// Infra failure (transient): record the incremented retry_count and preserve
		// the assignee so the retry stays reachable — clearing it unconditionally
		// would make the task unroutable and the retry silently never happen. Once
		// max_retries (or the pre-existing max_triggers cap) is reached the task
		// escalates out of the claim predicate: assignee cleared, previous_assignee
		// recorded, status in_progress and phase unchanged — non-terminal, but no
		// longer selected. See retry-bound.go.
		//
		// This branch deliberately keeps publishing its phase: the controller's
		// planning-retry gate reads phase from the published payload, not from disk.
		// It must therefore publish the phase this Job advanced to, if it advanced —
		// otherwise a run that moved planning -> execution and then failed would
		// publish the Job-start phase and rewind the task, re-running a phase that
		// already completed.
		frontmatter["status"] = "in_progress"
		if d.advancedPhase != "" {
			frontmatter["phase"] = d.advancedPhase
		}
		// Derive the streak from this Job's starting content, not from the generated
		// frontmatter: the fallback/passthrough/section generators already run
		// applyStatusFrontmatter, so reading the generated value would count one
		// failed Job twice and halve the effective bound.
		count, exhausted := nextRetryCount(frontmatterOf(d.originalContent))
		frontmatter[retryCountKey] = count
		if exhausted || triggerCountAtCap(frontmatter) {
			escalateFailure(frontmatter)
		}
		// phase is left as generated here — the planning-retry gate reads it from the
		// published payload, so unlike the other non-advancing branches this one
		// deliberately still publishes the Job-start phase.
	default:
		// Unknown status: defensive — surface to the operator like a cap exhaustion.
		frontmatter["status"] = "in_progress"
		if prev := string(frontmatter.Assignee()); prev != "" {
			frontmatter["previous_assignee"] = prev
		}
		frontmatter["assignee"] = ""
		// Advances nothing: omit the phase, same as the other preserving branches.
		delete(frontmatter, "phase")
	}
}

// applyResultMetrics writes the run's observed counts into the published frontmatter.
// It runs BEFORE applyResultFrontmatter so the entries ride every status path the
// deliverer publishes.
//
// Both values are optional: a nil field means the run produced no measurement (the
// turn summary reported nothing positive, or the transcript evidence was
// unavailable) and its key is omitted entirely — absence is never substituted with a
// zero. Values are written as plain integers; nothing else about the payload changes.
//
// A count already recorded on the task the run received is never lowered: when the
// recorded value is higher than the run's own evidence, the run's value is discarded
// and the recorded one stands. This is a guard on this deliverer's own output, not a
// merge — no read-modify-write of any task file, no second writer.
func (d *kafkaResultDeliverer) applyResultMetrics(
	frontmatter agentlib.TaskFrontmatter,
	result agentlib.AgentResultInfo,
) {
	if result.AgentTurns != nil {
		frontmatter[metricsAgentTurnsKey] = *result.AgentTurns
	}
	if result.InteractionCount == nil {
		return
	}
	if recorded, ok := recordedInteractionCount(d.originalContent); ok &&
		*result.InteractionCount < recorded {
		glog.V(2).Infof(
			"task %s: keeping recorded %s=%d, run observed %d",
			d.taskID,
			metricsInteractionCountKey,
			recorded,
			*result.InteractionCount,
		)
		return
	}
	frontmatter[metricsInteractionCountKey] = *result.InteractionCount
}

// recordedInteractionCount reads the interaction count already recorded on the task
// content the run received. ok is false when the key is absent, unparseable, or does
// not hold a non-negative integer.
func recordedInteractionCount(originalContent string) (int64, bool) {
	fm, _ := ParseMarkdownFrontmatter(originalContent)
	frontmatter := agentlib.TaskFrontmatter{}
	for k, v := range fm {
		frontmatter[k] = v
	}
	recorded, ok := frontmatter.Int(metricsInteractionCountKey)
	if !ok || recorded < 0 {
		return 0, false
	}
	return int64(recorded), true
}

// stampTargetVault adds target_vault to the task frontmatter from the original
// task content when the generated content lacks it. Stub results (failed /
// needs_input / unsupported-phase, empty or body-only Output) produce
// status-only generated frontmatter; without target_vault the controller's
// routing guard (routing.ShouldProcessResult) falls through to legacy routing
// and the non-owning controller scans-and-drops the result (spec 052). No-op
// when the generated content already carries target_vault (full results echo
// unchanged), or when originalContent has no frontmatter / no target_vault
// (legacy tasks and direct CLI runs keep today's routing).
func (d *kafkaResultDeliverer) stampTargetVault(frontmatter agentlib.TaskFrontmatter) {
	if _, ok := frontmatter["target_vault"]; ok {
		return
	}
	originalFM, _ := ParseMarkdownFrontmatter(d.originalContent)
	if tv, ok := originalFM["target_vault"].(string); ok && tv != "" {
		frontmatter["target_vault"] = tv
	}
}

// resolveNextPhase returns the validated phase string for a done agent result
// with a non-empty requested NextPhase (empty NextPhase is an in-place save and
// never reaches this function). An invalid value is logged with task-id context
// and falls back to "done" — we never refuse to write a result just because the
// agent requested a bogus phase.
func resolveNextPhase(
	taskID agentlib.TaskIdentifier,
	requested string,
) string {
	canonical, ok := domain.NormalizeTaskPhase(requested)
	if !ok {
		glog.Warningf("task %s: ignoring invalid NextPhase %q: unknown phase", taskID, requested)
		return "done"
	}
	return string(canonical)
}
