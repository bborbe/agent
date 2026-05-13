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
	"strings"

	"github.com/bborbe/errors"
	"github.com/golang/glog"
)

const (
	tailMaxLines = 5
	tailMaxBytes = 512
	tailJoiner   = " | "
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
	cmd, err := r.buildCommand(ctx, prompt)
	if err != nil {
		return nil, errors.Wrap(ctx, err, "build command")
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Wrap(ctx, err, "create stdout pipe")
	}

	if err := cmd.Start(); err != nil {
		return nil, errors.Wrap(ctx, err, "start claude CLI")
	}

	resultText, tail := scanOutput(ctx, stdoutPipe)

	if err := cmd.Wait(); err != nil {
		var tailMsg string
		if len(tail) > 0 {
			tailMsg = strings.Join(tail, tailJoiner)
		} else {
			tailMsg = "no stdout captured"
		}
		return nil, errors.Wrapf(ctx, err, "claude CLI failed: %s", tailMsg)
	}

	if resultText == "" {
		return nil, errors.New(ctx, "no result event found in claude CLI output")
	}

	return &ClaudeResult{Result: resultText}, nil
}

func (r *claudeRunner) buildCommand(
	ctx context.Context,
	prompt string,
) (*exec.Cmd, error) {
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
		workDir, err := r.config.WorkingDirectory.Resolve(ctx)
		if err != nil {
			return nil, errors.Wrap(ctx, err, "resolve WorkingDirectory")
		}
		cmd.Dir = workDir
	}

	cmd.Stdin = bytes.NewBufferString(prompt)

	cmd.Env = allowlistEnv()
	if r.config.ClaudeConfigDir != "" {
		cfgDir, err := r.config.ClaudeConfigDir.Resolve(ctx)
		if err != nil {
			return nil, errors.Wrap(ctx, err, "resolve ClaudeConfigDir")
		}
		cmd.Env = append(
			cmd.Env,
			"CLAUDE_CONFIG_DIR="+cfgDir,
		)
	}
	for k, v := range r.config.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	glog.V(4).Infof("env %+v", cmd.Env)

	return cmd, nil
}

// appendTail appends a non-empty line to the ring buffer, truncating to tailMaxBytes and evicting the oldest entry when over tailMaxLines.
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

// scanOutput reads stream-json lines from stdout, logs events, and returns the result text and a bounded tail of all non-empty lines.
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
	return resultText, tail
}

// allowlistEnv returns a minimal set of environment variables safe to pass to
// the Claude CLI subprocess. The parent process (task executor) typically runs
// with secrets, Kafka creds, and other sensitive vars in its environment; we
// do NOT want those flowing into Claude sessions by default. This allowlist
// enforces that trust boundary — only well-known, non-sensitive vars pass
// through automatically.
//
// To pass additional vars (e.g. GH_TOKEN for gh CLI auth), populate
// ClaudeRunnerConfig.Env, which is merged in after this allowlist.
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
