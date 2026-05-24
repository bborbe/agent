// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
		return nil, fmt.Errorf("%s marshal task: %w", s.cfg.Name, err)
	}

	prompt := BuildPrompt(s.cfg.Instructions, s.cfg.EnvContext, taskContent)

	result, runErr := s.cfg.Runner.Run(ctx, prompt)
	if runErr != nil {
		return &agentlib.Result{
			Status:  AgentStatusFailed,
			Message: fmt.Sprintf("%s pi run failed: %v", s.cfg.Name, runErr),
		}, nil
	}

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
	// Try ```json code block extraction first
	if jsonStr := extractFromCodeBlock(text); jsonStr != "" {
		var check struct{ Status string }
		if json.Unmarshal([]byte(jsonStr), &check) == nil {
			return jsonStr, true
		}
	}

	// Try direct JSON parse of the full text (handles single-line JSON)
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		var check struct{ Status string }
		if json.Unmarshal([]byte(trimmed), &check) == nil {
			return trimmed, true
		}
	}

	// Multi-line: find last { opening on its own line, scan to matching }
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		// Look for a line that is exactly "{"
		if line == "{" {
			// Found start, now find the closing }
			start := i
			depth := 0
			for end := start; end < len(lines); end++ {
				l := strings.TrimSpace(lines[end])
				if l == "{" {
					depth++
				} else if l == "}" {
					depth--
					if depth == 0 {
						objStr := strings.Join(lines[start:end+1], "\n")
						var r struct{ Status string }
						if json.Unmarshal([]byte(objStr), &r) == nil {
							return objStr, true
						}
						break
					}
				}
			}
			break
		}
		// Also handle single-char lines that are part of JSON
		if (strings.HasPrefix(line, "{") && !strings.Contains(line, ":")) ||
			(strings.HasSuffix(line, "}") && !strings.Contains(line, ",")) {
			// Found a lone { or } on a line
			if strings.HasPrefix(line, "{") {
				start := i
				depth := 0
				for end := start; end < len(lines); end++ {
					l := strings.TrimSpace(lines[end])
					if l == "{" {
						depth++
					} else if l == "}" {
						depth--
						if depth == 0 {
							objStr := strings.Join(lines[start:end+1], "\n")
							var r struct{ Status string }
							if json.Unmarshal([]byte(objStr), &r) == nil {
								return objStr, true
							}
							break
						}
					}
				}
			}
			break
		}
	}
	return "", false
}

// extractFromCodeBlock extracts JSON from the last ```json ... ``` block in text.
func extractFromCodeBlock(text string) string {
	start := strings.Index(text, "```json")
	if start < 0 {
		return ""
	}
	afterCodeFence := text[start+6:]
	endIdx := strings.Index(afterCodeFence, "```")
	if endIdx < 0 {
		return ""
	}
	codeContent := strings.TrimSpace(afterCodeFence[:endIdx])

	// The content might be a JSON string literal (with escaped newlines/quotes)
	if strings.HasPrefix(codeContent, "\"") {
		var decoded string
		if json.Unmarshal([]byte(codeContent), &decoded) == nil {
			return decoded
		}
	}
	return codeContent
}
