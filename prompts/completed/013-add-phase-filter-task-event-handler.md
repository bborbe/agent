---
status: completed
summary: Added phase filter to TaskEventHandler in prompt/controller to only process tasks with phases planning, in_progress, or ai_review, skipping nil phase and all other phases.
container: agent-013-add-phase-filter-task-event-handler
dark-factory-version: v0.69.0
created: "2026-03-29T00:00:00Z"
queued: "2026-03-29T10:40:38Z"
started: "2026-03-29T10:40:44Z"
completed: "2026-03-29T10:46:51Z"
---

<summary>
- Task events are only processed when their phase is planning, in_progress, or ai_review
- Task events with phase todo, human_review, done, or nil phase are silently skipped
- Existing status and assignee filters continue to work unchanged
- Tests cover all six phase values plus nil phase
- Existing passing tests are updated to include a qualifying phase value
</summary>

<objective>
Add a phase filter to the task event handler in prompt/controller so that only tasks in phases `planning`, `in_progress`, or `ai_review` generate prompts. Tasks with other phases or nil phase are skipped. This prevents prompt generation for tasks that are not yet ready for AI processing.
</objective>

<context>
Read CLAUDE.md for project conventions.

Key files to read before making changes:
- `prompt/controller/pkg/handler/task_event_handler.go` — current handler with status and assignee filters; needs phase filter added
- `prompt/controller/pkg/handler/task_event_handler_test.go` — Ginkgo tests; needs phase filter test cases and existing tests updated to include phase
- `lib/agent_task.go` — `Task` struct has `Phase *domain.TaskPhase` field (pointer, json omitempty)

Phase constants are defined in `github.com/bborbe/vault-cli/pkg/domain` (already an indirect dependency in prompt/controller/go.mod):
```go
TaskPhasePlanning    TaskPhase = "planning"
TaskPhaseInProgress  TaskPhase = "in_progress"
TaskPhaseAIReview    TaskPhase = "ai_review"
TaskPhaseTodo        TaskPhase = "todo"
TaskPhaseHumanReview TaskPhase = "human_review"
TaskPhaseDone        TaskPhase = "done"
```

The `Phase` field is a `*domain.TaskPhase` (pointer). Nil means no phase set — this must also be skipped.

The `domain.TaskPhases` type has a `.Contains(phase)` method that can be used for the allowlist check.
</context>

<requirements>
1. **Modify `prompt/controller/pkg/handler/task_event_handler.go`:**

   a. Add `"github.com/bborbe/vault-cli/pkg/domain"` to the import block.

   b. Add a package-level variable defining the allowed phases:

   ```go
   // allowedPhases lists the task phases that qualify for prompt generation.
   var allowedPhases = domain.TaskPhases{
       domain.TaskPhasePlanning,
       domain.TaskPhaseInProgress,
       domain.TaskPhaseAIReview,
   }
   ```

   c. Add a phase check in the `ConsumeMessage` method of `taskEventHandler`, immediately **after** the status check (`if task.Status != "in_progress"`) and **before** the assignee check. The check must handle nil phase and disallowed phases:

   ```go
   if task.Phase == nil || !allowedPhases.Contains(*task.Phase) {
       glog.V(3).Infof("skip task %s with phase %v", task.TaskIdentifier, task.Phase)
       return nil
   }
   ```

   The final filter order in `ConsumeMessage` should be:
   1. Empty message → skip
   2. Unmarshal error → skip
   3. Empty TaskIdentifier → skip
   4. Status != "in_progress" → skip
   5. **Phase nil or not in {planning, in_progress, ai_review} → skip** (NEW)
   6. Empty Assignee → skip
   7. Duplicate → skip
   8. Publish prompt

2. **Modify `prompt/controller/pkg/handler/task_event_handler_test.go`:**

   a. Add `"github.com/bborbe/vault-cli/pkg/domain"` to the import block.

   b. **Update ALL existing test cases** that build a qualifying task (status=in_progress, assignee set) to also set `Phase: domain.TaskPhaseInProgress.Ptr()` so they continue to pass the new phase filter. These are the tests that currently expect the task to reach the publisher or duplicate tracker:

   - "publishes prompt for qualifying task" — add `Phase: domain.TaskPhaseInProgress.Ptr()` to the task
   - "marks task as processed after successful publish" — add `Phase: domain.TaskPhaseInProgress.Ptr()` to the task
   - "does not mark task as processed when publish fails" — add `Phase: domain.TaskPhaseInProgress.Ptr()` to the task
   - "skips duplicate TaskIdentifier" — add `Phase: domain.TaskPhaseInProgress.Ptr()` to the task

   c. **Add new test cases for phase filtering** inside the existing `Describe("ConsumeMessage", ...)` block:

   ```go
   It("skips task with nil phase", func() {
       task := lib.Task{TaskIdentifier: "tid-phase-nil", Status: "in_progress", Phase: nil, Assignee: "claude"}
       err := h.ConsumeMessage(ctx, buildMsg(task))
       Expect(err).To(BeNil())
       Expect(fakePublisher.PublishPromptCallCount()).To(Equal(0))
   })

   It("skips task with phase todo", func() {
       task := lib.Task{TaskIdentifier: "tid-phase-todo", Status: "in_progress", Phase: domain.TaskPhaseTodo.Ptr(), Assignee: "claude"}
       err := h.ConsumeMessage(ctx, buildMsg(task))
       Expect(err).To(BeNil())
       Expect(fakePublisher.PublishPromptCallCount()).To(Equal(0))
   })

   It("skips task with phase human_review", func() {
       task := lib.Task{TaskIdentifier: "tid-phase-hr", Status: "in_progress", Phase: domain.TaskPhaseHumanReview.Ptr(), Assignee: "claude"}
       err := h.ConsumeMessage(ctx, buildMsg(task))
       Expect(err).To(BeNil())
       Expect(fakePublisher.PublishPromptCallCount()).To(Equal(0))
   })

   It("skips task with phase done", func() {
       task := lib.Task{TaskIdentifier: "tid-phase-done", Status: "in_progress", Phase: domain.TaskPhaseDone.Ptr(), Assignee: "claude"}
       err := h.ConsumeMessage(ctx, buildMsg(task))
       Expect(err).To(BeNil())
       Expect(fakePublisher.PublishPromptCallCount()).To(Equal(0))
   })

   It("publishes prompt for task with phase planning", func() {
       fakeTracker.IsDuplicateReturns(false)
       task := lib.Task{TaskIdentifier: "tid-phase-plan", Status: "in_progress", Phase: domain.TaskPhasePlanning.Ptr(), Assignee: "claude", Content: "plan it"}
       err := h.ConsumeMessage(ctx, buildMsg(task))
       Expect(err).To(BeNil())
       Expect(fakePublisher.PublishPromptCallCount()).To(Equal(1))
   })

   It("publishes prompt for task with phase in_progress", func() {
       fakeTracker.IsDuplicateReturns(false)
       task := lib.Task{TaskIdentifier: "tid-phase-ip", Status: "in_progress", Phase: domain.TaskPhaseInProgress.Ptr(), Assignee: "claude", Content: "do it"}
       err := h.ConsumeMessage(ctx, buildMsg(task))
       Expect(err).To(BeNil())
       Expect(fakePublisher.PublishPromptCallCount()).To(Equal(1))
   })

   It("publishes prompt for task with phase ai_review", func() {
       fakeTracker.IsDuplicateReturns(false)
       task := lib.Task{TaskIdentifier: "tid-phase-air", Status: "in_progress", Phase: domain.TaskPhaseAIReview.Ptr(), Assignee: "claude", Content: "review it"}
       err := h.ConsumeMessage(ctx, buildMsg(task))
       Expect(err).To(BeNil())
       Expect(fakePublisher.PublishPromptCallCount()).To(Equal(1))
   })
   ```

3. Do NOT modify any files outside `prompt/controller/pkg/handler/`.

4. Do NOT modify `lib/agent_task.go` or any domain types.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT modify `lib/` types — these are frozen
- Use `github.com/bborbe/vault-cli/pkg/domain` constants — never hardcode phase strings
- Use `domain.TaskPhases.Contains()` for the allowlist check — do not use a switch statement
- Use `github.com/bborbe/errors` for all error wrapping — never `fmt.Errorf`
- All tests must use external test packages (`package handler_test`)
- Existing tests must still pass after adding the phase filter
- The `Phase` field is `*domain.TaskPhase` (pointer) — nil check is required before dereferencing
- Use `.Ptr()` method on `domain.TaskPhase` constants to create pointers in tests (method exists on the type)
</constraints>

<verification>
Run `make test` in `prompt/controller/` — must pass with exit code 0:
```bash
cd prompt/controller && make test
```

Run `make precommit` in `prompt/controller/` — must pass with exit code 0:
```bash
cd prompt/controller && make precommit
```

Verify the phase check exists in the handler:
```bash
grep -n 'allowedPhases' prompt/controller/pkg/handler/task_event_handler.go
```
Expected: at least 2 matches (variable declaration and usage in ConsumeMessage).

Verify all phase test cases exist:
```bash
grep -c 'phase' prompt/controller/pkg/handler/task_event_handler_test.go
```
Expected: multiple matches covering nil, todo, human_review, done, planning, in_progress, ai_review.
</verification>
