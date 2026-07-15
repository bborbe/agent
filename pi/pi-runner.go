// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"

	"github.com/golang/glog"

	"github.com/bborbe/agent/envparse"
)

const (
	tailMaxLines = 5
	tailMaxBytes = 512
	tailJoiner   = " | "
)

//counterfeiter:generate -o ../mocks/pi-runner.go --fake-name PiRunner . Runner

// Runner spawns a headless Pi CLI session with a prompt and tools.
type Runner interface {
	Run(ctx context.Context, prompt string) (*Result, error)
}

// NewRunner creates a Runner that spawns pi --print.
func NewRunner(config PiRunnerConfig) Runner {
	return &piRunner{config: config}
}

type piRunner struct {
	config PiRunnerConfig
}

func (r *piRunner) Run(ctx context.Context, prompt string) (*Result, error) {
	cmd := r.buildCommand(ctx, prompt)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	resultText, tail := scanOutput(ctx, stdoutPipe)

	if err := cmd.Wait(); err != nil {
		var tailMsg string
		if len(tail) > 0 {
			tailMsg = strings.Join(tail, tailJoiner)
		} else {
			tailMsg = "no stdout captured"
		}
		return nil, &RunError{
			Msg: "pi CLI failed: " + tailMsg + " | stderr: " + stderrBuf.String(),
			Err: err,
		}
	}

	if resultText == "" {
		return nil, &RunError{Msg: "no result found in pi CLI output", Err: nil}
	}

	return &Result{Result: resultText}, nil
}

// RunError encapsulates a runner failure.
type RunError struct {
	Msg string
	Err error
}

func (e *RunError) Error() string { return e.Msg }

// Unwrap exposes the underlying cause so callers can use errors.Is/As.
func (e *RunError) Unwrap() error { return e.Err }

func (r *piRunner) buildCommand(ctx context.Context, prompt string) *exec.Cmd {
	args := []string{
		"--print",
		"--mode", "json",
		"--no-session",
	}

	if r.config.AllowedTools != "" {
		args = append(args, "--tools", r.config.AllowedTools)
	}

	if r.config.Model != "" {
		args = append(args, "--model", r.config.Model)
	}

	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, "pi", args...)

	// Set cwd to AgentDir so pi's context-file discovery (AGENTS.md /
	// CLAUDE.md walk from cwd toward /) finds the project guardrails.
	if r.config.AgentDir != "" {
		cmd.Dir = r.config.AgentDir
	}

	env := r.buildSubprocessEnv()
	cmd.Env = env

	glog.V(4).
		Infof("spawning pi: pi %v\n  cwd: %s\n  env: %v", args, cmd.Dir, envparse.RedactForLog(env))

	return cmd
}

func (r *piRunner) buildSubprocessEnv() []string {
	env := []string{}

	// Pass through safe vars and API keys.
	for _, k := range []string{
		"HOME", "PATH", "USER", "TZ", "ZONEINFO", "TMPDIR", "LANG", "LC_ALL",
		// API keys that may be needed by pi providers
		"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
		"OPENAI_API_KEY",
		"MINIMAX_API_KEY",
		"AZURE_OPENAI_API_KEY", "AZURE_OPENAI_BASE_URL",
		"DEEPSEEK_API_KEY", "GEMINI_API_KEY",
		"PROVIDER_API_KEY", "PROVIDER_BASE_URL",
	} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}

	// Consumer-provided env overrides.
	for k, v := range r.config.Env {
		env = append(env, k+"="+v)
	}

	return env
}

// piEvent represents a single event in the Pi stream-json output.
type piEvent struct {
	Type     string      `json:"type"`
	Message  piMessage   `json:"message"`
	Messages []piMessage `json:"messages"`
}

type piMessage struct {
	Role    string      `json:"role"`
	Content []piContent `json:"content"`
}

type piContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// appendTail appends a non-empty line to the ring buffer.
func appendTail(tail []string, line []byte) []string {
	if len(line) == 0 {
		return tail
	}
	captured := line
	if len(captured) > tailMaxBytes {
		captured = captured[:tailMaxBytes]
	}
	tail = append(tail, string(captured))
	if len(tail) > tailMaxLines {
		tail = tail[len(tail)-tailMaxLines:]
	}
	return tail
}

// scanOutput reads stream-json lines from stdout, logs events, and returns the result text.
func scanOutput(
	ctx context.Context,
	reader interface{ Read([]byte) (int, error) },
) (string, []string) {
	var resultText string
	var tail []string
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return "", nil
		default:
		}

		line := scanner.Bytes()
		glog.V(4).Infof("[line] %s", line)
		tail = appendTail(tail, line)

		var event piEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		if text := extractEventText(event); text != "" {
			resultText = text
		}
	}
	return resultText, tail
}

// extractEventText returns text from a piEvent, or empty string if none.
// Handles both agent_end (last assistant message's last text content) and
// message_update (any text content delta) event types.
func extractEventText(event piEvent) string {
	switch event.Type {
	case "agent_end":
		return lastAssistantText(event.Messages)
	case "message_update":
		return lastTextContent(event.Message.Content)
	}
	return ""
}

// lastAssistantText scans messages in reverse for the last assistant message,
// then returns its last non-empty text content.
func lastAssistantText(messages []piMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "assistant" {
			continue
		}
		return lastTextContent(messages[i].Content)
	}
	return ""
}

// lastTextContent returns the last non-empty text from content blocks.
func lastTextContent(content []piContent) string {
	var result string
	for _, c := range content {
		if c.Type == "text" && c.Text != "" {
			result = c.Text
		}
	}
	return result
}
