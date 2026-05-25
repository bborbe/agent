// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bborbe/errors"
	"github.com/golang/glog"

	agentlib "github.com/bborbe/agent/lib"
)

// StepConfig bundles everything a Pi agent Step needs at construction time.
type StepConfig struct {
	Name          string
	Runner        Runner
	Instructions  string
	EnvContext    map[string]string
	OutputSection string
	NextPhase     string
}

// NewStep wraps a single Pi invocation as an agentlib.Step.
func NewStep(cfg StepConfig) agentlib.Step {
	return &piStep{cfg: cfg}
}

type piStep struct {
	cfg StepConfig
}

func (s *piStep) Name() string { return s.cfg.Name }

func (s *piStep) ShouldRun(ctx context.Context, md *agentlib.Markdown) (bool, error) {
	_, exists := md.FindSection(s.cfg.OutputSection)
	return !exists, nil
}

func (s *piStep) Run(ctx context.Context, md *agentlib.Markdown) (*agentlib.Result, error) {
	taskContent, err := md.Marshal(ctx)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "%s marshal task", s.cfg.Name)
	}

	prompt := BuildPrompt(s.cfg.Instructions, s.cfg.EnvContext, taskContent)

	glog.V(2).Infof("%s: invoking pi runner (prompt=%d bytes)", s.cfg.Name, len(prompt))
	runStart := time.Now()
	result, runErr := s.cfg.Runner.Run(ctx, prompt)
	if runErr != nil {
		glog.V(2).Infof("%s: pi runner failed after %s: %v", s.cfg.Name, time.Since(runStart), runErr)
		return &agentlib.Result{
			Status:  AgentStatusFailed,
			Message: fmt.Sprintf("%s pi run failed: %v", s.cfg.Name, runErr),
		}, nil
	}
	glog.V(2).Infof("%s: pi runner returned %d bytes in %s", s.cfg.Name, len(result.Result), time.Since(runStart))

	// Parse the JSON result from pi output.
	jsonBlob, ok := extractLastJSONObject(result.Result)
	if !ok {
		return &agentlib.Result{
			Status:  AgentStatusFailed,
			Message: fmt.Sprintf("%s: no JSON result found in output: %.500s", s.cfg.Name, result.Result),
		}, nil
	}

	var piResult struct {
		Status  string   `json:"status"`
		Message string   `json:"message"`
		Files   []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(jsonBlob), &piResult); err != nil {
		return &agentlib.Result{
			Status:  AgentStatusFailed,
			Message: fmt.Sprintf("%s: parse result failed: %v (raw: %.500s)", s.cfg.Name, err, result.Result),
		}, nil
	}

	// Write the JSON blob as the output section body, not the raw assistant text.
	md.ReplaceSection(agentlib.Section{
		Heading: s.cfg.OutputSection,
		Body:    jsonBlob,
	})

	return &agentlib.Result{
		Status:    toAgentStatus(piResult.Status),
		NextPhase: s.cfg.NextPhase,
		Message:   piResult.Message,
	}, nil
}

func toAgentStatus(s string) agentlib.AgentStatus {
	switch s {
	case "done":
		return AgentStatusDone
	case "needs_input":
		return AgentStatusNeedsInput
	default:
		return AgentStatusFailed
	}
}

// extractLastJSONObject scans text for a JSON object.
// It first tries ```json code blocks, then falls back to a single-line
// or multi-line JSON object scan.
func extractLastJSONObject(text string) (string, bool) {
	if jsonStr := extractFromCodeBlock(text); jsonStr != "" && validateStatusJSON(jsonStr) {
		return jsonStr, true
	}

	if trimmed := tryTrimmedJSON(text); trimmed != "" {
		return trimmed, true
	}

	return tryMultilineJSON(strings.Split(text, "\n"))
}

// tryTrimmedJSON returns the trimmed text if it's a valid JSON object with a Status field.
func tryTrimmedJSON(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return ""
	}
	if !validateStatusJSON(trimmed) {
		return ""
	}
	return trimmed
}

// tryMultilineJSON scans lines in reverse for the last opening brace on its own line,
// then scans forward to the matching closing brace.
func tryMultilineJSON(lines []string) (string, bool) {
	start, ok := findOpeningBrace(lines)
	if !ok {
		return "", false
	}
	return matchJSONBlock(lines, start)
}

// findOpeningBrace returns the index of the last line that starts a JSON object.
// Recognizes both "{" alone and "{" without a ":" (e.g., "{foo").
func findOpeningBrace(lines []string) (int, bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "{" {
			return i, true
		}
		if isLooseOpenBrace(line) {
			return i, true
		}
		if isLooseCloseBrace(line) {
			return 0, false
		}
	}
	return 0, false
}

func isLooseOpenBrace(line string) bool {
	return strings.HasPrefix(line, "{") && !strings.Contains(line, ":")
}

func isLooseCloseBrace(line string) bool {
	return strings.HasSuffix(line, "}") && !strings.Contains(line, ",")
}

// matchJSONBlock scans lines from start forward for the matching closing brace
// and returns the joined JSON string if it parses with a Status field.
func matchJSONBlock(lines []string, start int) (string, bool) {
	depth := 0
	for end := start; end < len(lines); end++ {
		l := strings.TrimSpace(lines[end])
		switch l {
		case "{":
			depth++
		case "}":
			depth--
			if depth == 0 {
				objStr := strings.Join(lines[start:end+1], "\n")
				if validateStatusJSON(objStr) {
					return objStr, true
				}
				return "", false
			}
		}
	}
	return "", false
}

// validateStatusJSON returns true if s parses as a JSON object containing a Status field.
func validateStatusJSON(s string) bool {
	var check struct{ Status string }
	return json.Unmarshal([]byte(s), &check) == nil
}

// extractFromCodeBlock extracts JSON from the last ```json ... ``` block in text.
func extractFromCodeBlock(text string) string {
	_, afterOpenFence, found := strings.Cut(text, "```json")
	if !found {
		return ""
	}
	body, _, found := strings.Cut(afterOpenFence, "```")
	if !found {
		return ""
	}
	codeContent := strings.TrimSpace(body)

	// The content might be a JSON string literal (with escaped newlines/quotes)
	if strings.HasPrefix(codeContent, "\"") {
		var decoded string
		if json.Unmarshal([]byte(codeContent), &decoded) == nil {
			return decoded
		}
	}
	return codeContent
}
