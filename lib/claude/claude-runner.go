// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"

	"github.com/bborbe/errors"
	"github.com/golang/glog"
)

//counterfeiter:generate -o ../mocks/claude-claude-runner.go --fake-name ClaudeRunner . ClaudeRunner

// ClaudeRunner spawns a headless Claude Code CLI session with a prompt and MCP tools.
type ClaudeRunner interface {
	Run(ctx context.Context, prompt string) (*ClaudeResult, error)
}

// NewClaudeRunner creates a ClaudeRunner that spawns claude --print with MCP tools.
func NewClaudeRunner(config ClaudeRunnerConfig) ClaudeRunner {
	return &claudeRunner{
		config: config,
	}
}

type claudeRunner struct {
	config ClaudeRunnerConfig
}

func (r *claudeRunner) Run(ctx context.Context, prompt string) (*ClaudeResult, error) {
	cmd := r.buildCommand(ctx, prompt)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Wrap(ctx, err, "create stdout pipe")
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, errors.Wrap(ctx, err, "start claude CLI")
	}

	resultText := scanOutput(ctx, stdoutPipe)

	if err := cmd.Wait(); err != nil {
		return nil, errors.Wrapf(ctx, err, "claude CLI failed: %s", stderr.String())
	}

	if resultText == "" {
		return nil, errors.New(ctx, "no result event found in claude CLI output")
	}

	return &ClaudeResult{Result: resultText}, nil
}

func (r *claudeRunner) buildCommand(
	ctx context.Context,
	prompt string,
) *exec.Cmd {
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--verbose",
		"--strict-mcp-config",
	}

	if len(r.config.AllowedTools) > 0 {
		args = append(args, "--allowedTools", r.config.AllowedTools.String())
	}

	if r.config.Model != "" {
		args = append(args, "--model", r.config.Model.String())
	}

	glog.V(2).Infof("spawning claude CLI: claude %v", args)

	cmd := exec.CommandContext(ctx, "claude", args...)
	if r.config.WorkingDirectory != "" {
		cmd.Dir = r.config.WorkingDirectory.String()
	}

	cmd.Stdin = bytes.NewBufferString(prompt)

	cmd.Env = allowlistEnv()
	if r.config.ClaudeConfigDir != "" {
		cmd.Env = append(
			cmd.Env,
			"CLAUDE_CONFIG_DIR="+r.config.ClaudeConfigDir.String(),
		)
	}
	for k, v := range r.config.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	glog.V(4).Infof("env %+v", cmd.Env)

	return cmd
}

// scanOutput reads stream-json lines from stdout, logs events, and returns the result text.
func scanOutput(ctx context.Context, reader interface{ Read([]byte) (int, error) }) string {
	var resultText string
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ""
		default:
		}

		line := scanner.Bytes()
		glog.V(4).Infof("[line] %s", line)

		var event claudeEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		if event.Type == "result" && event.Result != "" {
			resultText = event.Result
		}

		for _, c := range event.Message.Content {
			switch c.Type {
			case "tool_use":
				logToolUse(c)
			default:
				glog.V(2).Infof("type(%s): %s", c.Type, c.Text)
			}
		}
	}
	return resultText
}

// allowlistEnv returns a minimal set of environment variables safe to pass to subprocesses.
func allowlistEnv() []string {
	keys := []string{
		"HOME", "PATH", "USER", "TZ", "ZONEINFO", "TMPDIR", "LANG", "LC_ALL",
	}
	var env []string
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}
