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

	"github.com/bborbe/agent/envparse"
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
		glog.V(2).Infof("cmd.Dir = %v", cmd.Dir)
	}

	cmd.Stdin = bytes.NewBufferString(prompt)
	glog.V(3).Infof("cmd.Stdin = %v", prompt)

	env, err := r.buildSubprocessEnv(ctx)
	if err != nil {
		return nil, errors.Wrap(ctx, err, "build subprocess env")
	}
	cmd.Env = env
	glog.V(2).Infof("cmd.Env = %+v", envparse.RedactForLog(cmd.Env))

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

// buildSubprocessEnv constructs the env var slice for the Claude CLI subprocess.
// Precedence (later layers override earlier):
//
//  1. Allowlist: pass-through of safe parent-process vars (HOME, PATH, ...).
//     The parent process (task executor) typically runs with secrets, Kafka creds,
//     and other sensitive vars in its environment; we do NOT want those flowing into
//     Claude sessions by default. This allowlist enforces that trust boundary — only
//     well-known, non-sensitive vars pass through automatically.
//  2. CLAUDE_CONFIG_DIR: explicit config > parent process env > default "~/.claude".
//  3. Consumer-provided r.config.Env: arbitrary overrides — highest precedence.
//     To pass additional vars (e.g. GH_TOKEN for gh CLI auth), populate
//     ClaudeRunnerConfig.Env. Values here cross the trust boundary into the
//     Claude CLI subprocess — only add what the agent genuinely needs.
//
// Building via map[string]string makes precedence linear by assignment order and
// prevents duplicate-key entries in the resulting []string.
func (r *claudeRunner) buildSubprocessEnv(ctx context.Context) ([]string, error) {
	env := map[string]string{}

	// Layer 1: allowlist pass-through.
	for _, k := range []string{"HOME", "PATH", "USER", "TZ", "ZONEINFO", "TMPDIR", "LANG", "LC_ALL"} {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}

	// Layer 2: CLAUDE_CONFIG_DIR with precedence config > env > default.
	cfgDir := r.config.ClaudeConfigDir
	if cfgDir == "" {
		if envVal := os.Getenv("CLAUDE_CONFIG_DIR"); envVal != "" {
			cfgDir = ClaudeConfigDir(envVal)
		}
	}
	if cfgDir == "" {
		cfgDir = "~/.claude"
	}
	resolved, err := cfgDir.Resolve(ctx)
	if err != nil {
		return nil, errors.Wrap(ctx, err, "resolve ClaudeConfigDir")
	}
	env["CLAUDE_CONFIG_DIR"] = resolved

	// Layer 3: consumer-provided env overrides everything above.
	for k, v := range r.config.Env {
		env[k] = v
	}

	// Convert to []string for exec.Cmd.
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result, nil
}
