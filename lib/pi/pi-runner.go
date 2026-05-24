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
	cmd, err := r.buildCommand(ctx, prompt)
	if err != nil {
		return nil, err
	}

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
		return nil, &RunError{Msg: "pi CLI failed: " + tailMsg + " | stderr: " + stderrBuf.String(), Err: err}
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

func (r *piRunner) buildCommand(ctx context.Context, prompt string) (*exec.Cmd, error) {
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

	// pi --print expects the prompt as a positional argument, not via stdin.
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, "pi", args...)

	env := r.buildSubprocessEnv()
	cmd.Env = env

	glog.V(4).Infof("spawning pi: pi %v\n  env: %v", args, env)

	return cmd, nil
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
	Type     string     `json:"type"`
	Message  piMessage  `json:"message"`
	Messages []piMessage `json:"messages"`
}

type piMessage struct {
	Role    string       `json:"role"`
	Content []piContent  `json:"content"`
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
func scanOutput(ctx context.Context, reader interface{ Read([]byte) (int, error) }) (string, []string) {
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

		// Extract result from agent_end messages.
		if event.Type == "agent_end" && len(event.Messages) > 0 {
			// Find last assistant message.
			for i := len(event.Messages) - 1; i >= 0; i-- {
				msg := event.Messages[i]
				if msg.Role == "assistant" {
					for j := len(msg.Content) - 1; j >= 0; j-- {
						if msg.Content[j].Type == "text" && msg.Content[j].Text != "" {
							resultText = msg.Content[j].Text
							break
						}
					}
					break
				}
			}
		}

		// Also capture text deltas from message_update for streaming.
		if event.Type == "message_update" {
			for _, c := range event.Message.Content {
				if c.Type == "text" && c.Text != "" {
					// Keep last text content seen.
					resultText = c.Text
				}
			}
		}
	}
	return resultText, tail
}
