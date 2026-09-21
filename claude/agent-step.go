// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bborbe/collection"
	"github.com/bborbe/errors"
	"github.com/golang/glog"

	agentlib "github.com/bborbe/agent"
)

// AgentStepConfig bundles everything an agent Step needs at construction time.
//
// The Runner is pre-configured with AllowedTools + Model via ClaudeRunnerConfig
// at construction; per-step Instructions + EnvContext are supplied here so a
// single Runner can serve multiple steps with different prompts.
type AgentStepConfig struct {
	// Name is the step name for logs.
	Name string

	// Runner is the Claude CLI invocation backend (pre-configured with
	// AllowedTools, Model, ClaudeConfigDir via ClaudeRunnerConfig).
	Runner ClaudeRunner

	// Instructions is the system prompt for this step.
	Instructions Instructions

	// EnvContext is forwarded to the Claude CLI tool-invocation environment.
	EnvContext map[string]string

	// OutputSection is the body section heading for the LLM's output
	// (e.g. "## Analysis", "## Review").
	OutputSection string

	// NextPhase is the phase to advance to on success. Empty means
	// in-place save (multi-step phase intermediate).
	NextPhase string
}

// NewAgentStep wraps a single Claude invocation as an agentlib.Step.
//
// Used by AI-heavy agents (trade-analysis, pr-reviewer style). The LLM
// reads the marshaled task content (frontmatter + body) and writes its
// output verbatim under the configured section heading.
//
// For boundary parsing (markdown → typed Go struct), use
// agentlib.NewParseStep with an AIParser implementation instead.
func NewAgentStep(cfg AgentStepConfig) agentlib.Step {
	return &agentStep{cfg: cfg}
}

type agentStep struct {
	cfg AgentStepConfig
}

// failureSectionHeading is the repo-wide failure-marker heading written by
// delivery/content-generator.go (and agent-task-executor's result publisher)
// on AgentStatusFailed / AgentStatusNeedsInput. Its presence means the prior
// run failed, so ShouldRun forces a re-run instead of treating the output
// section as completed work (spec 051).
const failureSectionHeading = "## Failure"

// Name implements agentlib.Step.
func (s *agentStep) Name() string { return s.cfg.Name }

// ShouldRun returns false only when the output section exists and represents a
// genuine success: a body that is not a needs_input/failed AgentResult, with no
// ## Failure section present.
//
// Single-step idempotency check: if the LLM already wrote its section in a
// prior Job that crashed before phase advance, skip the re-invocation.
// (For multi-step phases, decompose the work — don't rely on a single
// AgentStep to be partially-resumable.)
//
// A failure marker — a ## Failure section, or an output-section body that
// parses to a needs_input/failed AgentResult — forces a re-run: a failed run
// is not completed work, and re-dispatch must re-invoke claude (spec 051).
func (s *agentStep) ShouldRun(_ context.Context, md *agentlib.Markdown) (bool, error) {
	_, exists := md.FindSection(s.cfg.OutputSection)
	if !exists {
		return true, nil
	}
	if s.failureMarked(md) {
		return true, nil
	}
	return false, nil
}

// failureMarked reports whether the task carries a failure marker that must
// force a re-run despite an existing output section: a ## Failure section, or
// an output-section body that parses to a needs_input/failed AgentResult.
// Bodies that are not a failure marker (done JSON, unparseable prose, unknown
// status) are treated as a genuine success section (spec 051).
func (s *agentStep) failureMarked(md *agentlib.Markdown) bool {
	if _, exists := md.FindSection(failureSectionHeading); exists {
		return true
	}
	section, exists := md.FindSection(s.cfg.OutputSection)
	if !exists {
		return false
	}
	result, ok := parseAgentResultBody(section.Body)
	if !ok {
		return false
	}
	return result.Status == agentlib.AgentStatusNeedsInput ||
		result.Status == agentlib.AgentStatusFailed
}

// parseAgentResultBody extracts the last balanced JSON object from body and
// unmarshals it as an AgentResult. ok is false when no JSON object is present
// or unmarshal fails — callers treat an unparseable body as "not a failure
// marker" (best-effort parsing, spec 051). Unknown status values are not a
// failure marker and fall through to the success/skip path.
func parseAgentResultBody(body string) (AgentResult, bool) {
	blob, ok := extractLastJSONObject(body)
	if !ok {
		return AgentResult{}, false
	}
	var result AgentResult
	if err := json.Unmarshal([]byte(blob), &result); err != nil {
		return AgentResult{}, false
	}
	return result, true
}

// stripLeadingSectionHeading drops a leading markdown section heading from an
// extracted payload so the payload can be written as a section body.
//
// Marshal emits a section as Heading followed by Body, so a payload that
// supplies its own heading would be written twice: the section would carry the
// heading twice, the next phase's reader would bound the section at the second
// one and find an empty body, and parsing would fail with "json block missing"
// (observed on dev 2026-09-21, Job
// trading-hypothesis-agent-b9111443-202609212037). The payload legitimately
// carries its own heading — AgentResult.Output is documented as "typically a
// heading plus a fenced JSON block" — so the heading is stripped here rather
// than at the producer.
//
// A leading "# " or "## " heading is dropped, together with any blank line
// immediately following it, so the body begins with the payload's remaining
// content (typically a fenced JSON block). A "### " or deeper sub-heading does
// not start a section and is kept. Payloads that do not begin with a heading
// are returned unchanged.
func stripLeadingSectionHeading(payload string) string {
	firstLine, rest, found := strings.Cut(payload, "\n")
	if !strings.HasPrefix(firstLine, "#") {
		return payload
	}
	if !agentlib.IsMarkdownSectionHeading(strings.TrimSpace(firstLine)) {
		return payload
	}
	if !found {
		return ""
	}
	return strings.TrimPrefix(rest, "\n")
}

// runCounts extracts the run's observed counts from the CLI result with the absence
// rules: a turn total that is not positive is no measurement (nil), and a nil
// InteractionCount (evidence unavailable) stays nil — absence is never turned into a
// zero (spec 053).
func runCounts(result *ClaudeResult) (agentTurns *int64, interactionCount *int64) {
	if result == nil {
		return nil, nil
	}
	if result.NumTurns > 0 {
		agentTurns = collection.Ptr(result.NumTurns)
	}
	return agentTurns, result.InteractionCount
}

// Run marshals the task, calls Claude with the step's prompt + tools, and
// writes the agent's declared payload — the `output` field of its result
// envelope — under the configured section heading. A runner result that is not
// an envelope, or an envelope without `output`, is written raw. On a
// needs_input/failed runner body the step returns that status WITHOUT writing
// the output section — the deliverer writes the ## Failure marker instead, so
// the section is never left looking like completed work (spec 051).
func (s *agentStep) Run(ctx context.Context, md *agentlib.Markdown) (*agentlib.Result, error) {
	taskContent, err := md.Marshal(ctx)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "%s marshal task", s.cfg.Name)
	}

	prompt := BuildPrompt(s.cfg.Instructions.String(), s.cfg.EnvContext, taskContent)

	glog.Infof("%s: invoking claude runner (prompt=%d bytes)", s.cfg.Name, len(prompt))
	runStart := time.Now()
	result, runErr := s.cfg.Runner.Run(ctx, prompt)
	if runErr != nil {
		glog.Infof(
			"%s: claude runner failed after %s: %v",
			s.cfg.Name,
			time.Since(runStart),
			runErr,
		)
		return &agentlib.Result{
			Status:  agentlib.AgentStatusFailed,
			Message: fmt.Sprintf("%s claude run failed: %v", s.cfg.Name, runErr),
		}, nil
	}
	glog.Infof(
		"%s: claude runner returned %d bytes in %s",
		s.cfg.Name,
		len(result.Result),
		time.Since(runStart),
	)

	agentTurns, interactionCount := runCounts(result)

	// Parse the runner's result once: the same envelope decides the
	// needs_input/failed path below and carries the payload written on the
	// success path further down.
	parsed, parsedOK := parseAgentResultBody(result.Result)

	// A needs_input/failed body is a failed run, not completed work — return
	// that status and let the deliverer write the ## Failure marker. Never
	// write a success-looking output section for a failed run: that section
	// would make ShouldRun skip every subsequent re-dispatch (spec 051).
	if parsedOK &&
		(parsed.Status == agentlib.AgentStatusNeedsInput ||
			parsed.Status == agentlib.AgentStatusFailed) {
		msg := parsed.Message
		if msg == "" {
			msg = fmt.Sprintf("%s claude run returned status %s", s.cfg.Name, parsed.Status)
		}
		return &agentlib.Result{
			Status:           parsed.Status,
			Message:          msg,
			AgentTurns:       agentTurns,
			InteractionCount: interactionCount,
		}, nil
	}

	// Write the payload the agent declared — the envelope's `output` field
	// (a heading plus a fenced JSON block), not the envelope itself. The next
	// phase parses this section directly and cannot unwrap an envelope first,
	// so writing the envelope strands the pipeline (observed on dev
	// 2026-09-21: "plan section invalid: json block missing in plan section").
	// A result that is not an envelope, or an envelope without `output`, keeps
	// its raw runner text so prompts that do not use the envelope are unaffected.
	//
	// The extracted payload carries its own heading (see AgentResult.Output);
	// stripLeadingSectionHeading removes it before it is written as a body, so
	// marshalling cannot emit the heading twice and bound the section to an
	// empty region. The raw-text fallback path is left as-is: it is not the
	// documented envelope payload shape and stripping it would change behaviour
	// for prompts that never opted into the envelope.
	//
	// ShouldRun's failure detection parses this body with parseAgentResultBody
	// and forces a re-run on a needs_input/failed status (spec 051). The
	// extracted payload is the agent's declared domain output — the fenced JSON
	// carries the phase's own schema (e.g. a plan), which has no `status` field
	// of the AgentStatus enum. Unmarshaling it into AgentResult therefore yields
	// an empty status, neither needs_input nor failed, so the section is read
	// back as genuine success and does not force a re-run on every dispatch.
	body := result.Result
	if parsedOK && parsed.Output != "" {
		body = stripLeadingSectionHeading(parsed.Output)
	}

	md.ReplaceSection(agentlib.Section{
		Heading: s.cfg.OutputSection,
		Body:    body,
	})

	return &agentlib.Result{
		Status:           agentlib.AgentStatusDone,
		NextPhase:        s.cfg.NextPhase,
		AgentTurns:       agentTurns,
		InteractionCount: interactionCount,
	}, nil
}
