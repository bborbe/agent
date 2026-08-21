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

// partialMaxBytes caps the partial captured from the claude CLI stream: at most this
// many of the most recent bytes of streamed assistant text are kept. It is a frozen
// package constant (spec 049): a cap that can be disabled is an escape hatch on the
// salvage feature, so it is deliberately NOT configurable.
const partialMaxBytes = 16384

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

	resultText, usage, partial, tail := scanOutput(ctx, stdoutPipe)

	if err := cmd.Wait(); err != nil {
		var tailMsg string
		if len(tail) > 0 {
			tailMsg = strings.Join(tail, tailJoiner)
		} else {
			tailMsg = "no stdout captured"
		}
		return &ClaudeResult{
			Partial: partial,
		}, errors.Wrapf(ctx, err, "claude CLI failed: %s", tailMsg)
	}

	if resultText == "" {
		return &ClaudeResult{
			Partial: partial,
		}, errors.New(ctx, "no result event found in claude CLI output")
	}

	return &ClaudeResult{
		Result:              resultText,
		Partial:             partial,
		InputTokens:         usage.inputTokens,
		OutputTokens:        usage.outputTokens,
		CacheCreationTokens: usage.cacheCreationTokens,
		CacheReadTokens:     usage.cacheReadTokens,
		NumTurns:            usage.numTurns,
	}, nil
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

// appendPartial appends chunk to the bounded partial buffer, keeping at most
// partialMaxBytes of the most recent bytes and dropping the earliest bytes on
// overflow.
func appendPartial(partial []byte, chunk string) []byte {
	partial = append(partial, chunk...)
	if len(partial) > partialMaxBytes {
		partial = partial[len(partial)-partialMaxBytes:]
	}
	return partial
}

// capturePartial appends the plain text content of an assistant event to the
// bounded partial buffer. Tool payloads (tool_use items carry their payload in
// Input, not Text), usage telemetry, and the stream-json envelope itself are
// excluded. On event-schema drift the gate degrades to a no-op without crashing.
func capturePartial(partial []byte, event claudeEvent) []byte {
	if event.Type == "assistant" {
		for _, c := range event.Message.Content {
			if c.Type == "text" {
				partial = appendPartial(partial, c.Text)
			}
		}
	}
	return partial
}

// parseUsage extracts token counts from a raw usage JSON block and the num_turns field
// from the parent event. Each token field is unmarshalled individually from a map so that a
// decode error on one field does not roll back valid values from other fields.
// Malformed values degrade to 0 without error, per the best-effort telemetry contract.
func parseUsage(usageRaw json.RawMessage, numTurns json.Number) sessionUsage {
	var usage sessionUsage
	var usageMap map[string]json.RawMessage
	if err := json.Unmarshal(usageRaw, &usageMap); err != nil {
		return usage
	}

	var inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens json.Number

	//nolint:errcheck // Intentional: malformed fields degrade to 0, which is the correct behaviour.
	if v, ok := usageMap["input_tokens"]; ok {
		json.Unmarshal(v, &inputTokens)
	}
	//nolint:errcheck
	if v, ok := usageMap["output_tokens"]; ok {
		json.Unmarshal(v, &outputTokens)
	}
	//nolint:errcheck
	if v, ok := usageMap["cache_creation_input_tokens"]; ok {
		json.Unmarshal(v, &cacheCreationTokens)
	}
	//nolint:errcheck
	if v, ok := usageMap["cache_read_input_tokens"]; ok {
		json.Unmarshal(v, &cacheReadTokens)
	}

	usage.inputTokens = numberToInt64(inputTokens)
	usage.outputTokens = numberToInt64(outputTokens)
	usage.cacheCreationTokens = numberToInt64(cacheCreationTokens)
	usage.cacheReadTokens = numberToInt64(cacheReadTokens)
	usage.numTurns = numberToInt64(numTurns)
	return usage
}

// scanOutput reads stream-json lines from stdout, logs events, and returns the result
// text, the captured usage summary, the bounded partial of streamed assistant text,
// and a bounded tail of all non-empty lines.
func scanOutput(
	ctx context.Context,
	reader interface{ Read([]byte) (int, error) },
) (string, sessionUsage, string, []string) {
	var resultText string
	var usage sessionUsage
	var partial []byte
	var tail []string
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return resultText, usage, string(partial), tail
		default:
		}

		line := scanner.Bytes()
		glog.V(4).Infof("[line] %s", line)

		tail = appendTail(tail, line)

		// Two-pass unmarshal: first extract type/result safely (never fails on schema issues),
		// then attempt full unmarshal for usage fields (may fail on e.g. json.Number receiving
		// a string). The first pass always succeeds for syntactically valid JSON, ensuring
		// resultText survives schema drift in the usage subtree.
		var holder resultHolder
		if err := json.Unmarshal(line, &holder); err != nil {
			continue
		}

		if holder.Type == "result" && holder.Result != "" {
			resultText = holder.Result
		}

		// Full unmarshal: may fail due to schema-level issues in usage fields.
		// On failure we still keep the resultText captured above.
		var event claudeEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		// Usage capture is deliberately gated on the presence of a usage object, NOT
		// on a non-empty result text: a later result event carrying fresh usage but an
		// empty result string must update the numbers while leaving the previously
		// captured text intact. Last usage object wins.
		if event.Type == "result" && len(event.Usage) > 0 {
			usage = parseUsage(event.Usage, event.NumTurns)
		}

		partial = capturePartial(partial, event)

		for _, c := range event.Message.Content {
			switch c.Type {
			case "tool_use":
				logToolUse(c)
			default:
				glog.V(2).Infof("type(%s): %s", c.Type, c.Text)
			}
		}
	}
	return resultText, usage, string(partial), tail
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
