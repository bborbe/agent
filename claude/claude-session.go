// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/bborbe/errors"
	"github.com/golang/glog"

	agentlib "github.com/bborbe/agent"
)

const (
	// sessionCloseGrace bounds how long Close waits for the held process to exit on
	// its own after its stdin has been closed, before it is killed.
	sessionCloseGrace = 2 * time.Second
	// sessionEventBufferSize and sessionMaxEventSize bound the stdout scanner the
	// session reads. They mirror the one-shot runner's buffer, so a stream-json line
	// the runner can carry cannot fail the session for its size.
	sessionEventBufferSize = 1024 * 1024
	sessionMaxEventSize    = 10 * 1024 * 1024
	// permissionPreviewMaxBytes caps the tool-input preview carried out of the
	// process to the decider. The preview is never logged.
	permissionPreviewMaxBytes = 512
	// permissionSubtypePrimary and permissionSubtypeFallback are the two request
	// subtypes the CLI has used to ask for a tool decision. can_use_tool is the
	// primary spelling; permission is accepted as the fallback.
	permissionSubtypePrimary  = "can_use_tool"
	permissionSubtypeFallback = "permission"
)

// PermissionRequest describes a tool invocation the CLI raised mid-turn and is
// waiting on. It carries no conversation content: only the tool's name, a
// human-readable description, and a preview of the tool input.
type PermissionRequest struct {
	// ToolName is the name of the tool the CLI wants to invoke.
	ToolName string
	// Description is the CLI's human-readable description of the invocation.
	Description string
	// InputPreview is a preview of the tool input. It is not logged.
	InputPreview string
}

// PermissionDecision is the verdict returned for a PermissionRequest.
type PermissionDecision struct {
	// Allow reports whether the tool invocation may proceed.
	Allow bool
	// Message is an optional explanation returned to the CLI on denial.
	Message string
}

//counterfeiter:generate -o ../mocks/claude-permission-decider.go --fake-name ClaudePermissionDecider . PermissionDecider

// PermissionDecider decides whether a tool invocation the CLI raised mid-turn is
// allowed. It is the one place this package accepts an instruction from outside
// the process. The pod's HTTP surface implements it; a nil decider is invalid.
type PermissionDecider interface {
	DecidePermission(ctx context.Context, request PermissionRequest) (PermissionDecision, error)
}

// NewSession returns a Session backed by one long-lived claude process.
//
// The process is started on the first Prompt and held for the session's life, so a
// second prompt reaches the same process and the same conversation. The session's
// constructor performs no I/O because the shared session interface's factory is
// construction-only by contract; the spawn is therefore deferred to the first turn.
//
// config is the same ClaudeRunnerConfig the one-shot runner takes, so there is no
// second configuration to keep in sync. decider receives every mid-turn tool
// decision; a nil decider fails a turn that raises one rather than blocking.
func NewSession(config ClaudeRunnerConfig, decider PermissionDecider) agentlib.Session {
	return &session{config: config, decider: decider}
}

// NewSessionFactory returns a SessionFactory that builds one claude-backed session
// per session id.
//
// Every id gets its own session and therefore its own process. The id is not part
// of the spawn: continuity comes from the held process, not from a CLI flag, so
// the argv is identical for every id and no id can reach the argument vector.
func NewSessionFactory(base ClaudeRunnerConfig, decider PermissionDecider) agentlib.SessionFactory {
	return &sessionFactory{base: base, decider: decider}
}

type sessionFactory struct {
	base    ClaudeRunnerConfig
	decider PermissionDecider
}

// Create returns the session for id, building it on first use.
func (f *sessionFactory) Create(_ string) agentlib.Session {
	return NewSession(f.base, f.decider)
}

type session struct {
	config  ClaudeRunnerConfig
	decider PermissionDecider

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	closed  bool
}

// Prompt sends one turn to the held process and returns that turn's answer.
//
// The first call starts the process; every later call writes to the same one. A
// turn that ends abnormally — a cancelled context, a malformed event, or the
// process dying — closes the session, so a later call fails instead of silently
// starting a fresh conversation behind the caller's back.
func (s *session) Prompt(ctx context.Context, prompt string) (string, error) {
	cmd, stdin, scanner, err := s.process(ctx)
	if err != nil {
		return "", err
	}

	result, err := s.turn(ctx, cmd, stdin, scanner, prompt)
	if err != nil {
		s.abort(cmd)
		return "", err
	}
	return result, nil
}

// process returns the held process, starting it on first use.
func (s *session) process(
	ctx context.Context,
) (*exec.Cmd, io.WriteCloser, *bufio.Scanner, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, nil, nil, errors.New(ctx, "claude session is closed")
	}
	if s.cmd == nil {
		cmd, stdin, scanner, err := startProcess(ctx, s.config, s.decider)
		if err != nil {
			s.closed = true
			return nil, nil, nil, errors.Wrap(ctx, err, "start claude process")
		}
		s.cmd, s.stdin, s.scanner = cmd, stdin, scanner
	}
	return s.cmd, s.stdin, s.scanner, nil
}

// abort marks the session unusable and reaps the process. It is the single exit a
// failed turn takes, so no failure path can leave the session reusable.
func (s *session) abort(cmd *exec.Cmd) {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
}

// Close terminates the held process.
//
// It closes the process's stdin first — the CLI's input stream ends and it exits on
// its own — waits for it with a bounded grace period, and kills it only if it is
// still running. Close is idempotent: a second call returns without blocking.
func (s *session) Close(ctx context.Context) error {
	cmd, stdin := s.detach()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if stdin != nil {
		_ = stdin.Close()
	}

	graceCtx, cancelGrace := context.WithTimeout(ctx, sessionCloseGrace)
	defer cancelGrace()
	stop := context.AfterFunc(graceCtx, func() { _ = cmd.Process.Kill() })
	defer stop()

	_ = cmd.Wait()
	return nil
}

// detach marks the session closed and returns the held process, or nil when there
// is nothing to close. It is idempotent.
func (s *session) detach() (*exec.Cmd, io.WriteCloser) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, nil
	}
	s.closed = true
	return s.cmd, s.stdin
}

// turn performs exactly one turn: one input line written to the process's stdin,
// then stdout read until that turn's terminal event.
func (s *session) turn(
	ctx context.Context,
	cmd *exec.Cmd,
	stdin io.WriteCloser,
	scanner *bufio.Scanner,
	prompt string,
) (string, error) {
	if err := writeTurn(ctx, stdin, prompt); err != nil {
		return "", errors.Wrap(ctx, err, "write turn")
	}

	// A blocked scanner read cannot observe context cancellation by itself, so the
	// send's context is watched: on cancellation the process is asked to terminate
	// (SIGTERM, so a graceful exit is still possible), its stdout closes and the read
	// returns. The watcher is stopped when the turn ends, so it never outlives it.
	stopWatch := context.AfterFunc(ctx, func() { _ = cmd.Process.Signal(syscall.SIGTERM) })
	defer stopWatch()

	return s.readTurn(ctx, cmd, stdin, scanner)
}

// readTurn reads stdout until the turn's terminal event, routing any permission
// request it meets out to the decider and writing the verdict back.
func (s *session) readTurn(
	ctx context.Context,
	cmd *exec.Cmd,
	stdin io.WriteCloser,
	scanner *bufio.Scanner,
) (string, error) {
	for scanner.Scan() {
		event, err := parseSessionEvent(ctx, scanner.Bytes())
		if err != nil {
			return "", err
		}
		if err := s.handle(ctx, stdin, event); err != nil {
			return "", err
		}
		if event.Type == "result" {
			return event.Result, nil
		}
	}
	return "", endOfStream(ctx, cmd)
}

// handle reacts to one event: a control request is routed to the decider and
// answered; every other event is a conversation event the turn ignores.
func (s *session) handle(
	ctx context.Context,
	stdin io.WriteCloser,
	event sessionEvent,
) error {
	if event.Type != "control_request" {
		return nil
	}
	if event.Request == nil || !event.Request.isPermissionRequest() {
		return errors.Errorf(ctx, "unsupported control request from claude process")
	}
	return s.answerPermission(ctx, stdin, event)
}

// answerPermission asks the decider for a verdict and writes it back to the same
// process's stdin. The turn blocks on the decider; the pause is the held process
// waiting on the answer, not a busy loop. A decider error fails the turn — it never
// auto-allows, auto-denies or retries.
func (s *session) answerPermission(
	ctx context.Context,
	stdin io.WriteCloser,
	event sessionEvent,
) error {
	if s.decider == nil {
		return errors.New(ctx, "permission requested but no decider is configured")
	}
	decision, err := s.decider.DecidePermission(ctx, PermissionRequest{
		ToolName:     event.Request.ToolName,
		Description:  event.Request.Description,
		InputPreview: previewOf(event.Request.Input),
	})
	if err != nil {
		return errors.Wrap(ctx, err, "decide permission")
	}
	return writePermissionResponse(ctx, stdin, event.RequestID, decision)
}

// endOfStream reports why a turn ended without its terminal event: a cancelled
// context, or a process that exited. It never reports a turn as successful.
func endOfStream(ctx context.Context, cmd *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return errors.Wrap(ctx, err, "turn cancelled")
	}
	waitErr := cmd.Wait()
	if waitErr == nil {
		waitErr = errors.New(ctx, "claude process closed its output")
	}
	return errors.Wrap(ctx, waitErr, "claude process ended mid-turn")
}

// sessionEvent is one stream-json line as the session reads it: the runner's
// claudeEvent vocabulary plus the control-request envelope the CLI uses to raise a
// tool decision mid-turn. The vocabulary is shared with the one-shot runner rather
// than forked, so a change to the CLI's event shape has one place to land.
type sessionEvent struct {
	claudeEvent
	// RequestID identifies a control request, and is echoed back on the response.
	RequestID string `json:"request_id"`
	// Request carries the control request's payload. Nil on conversation events.
	Request *controlRequest `json:"request"`
}

// controlRequest is the payload of a control_request event.
type controlRequest struct {
	Subtype     string          `json:"subtype"`
	ToolName    string          `json:"tool_name"`
	Description string          `json:"description"`
	Input       json.RawMessage `json:"input"`
}

// isPermissionRequest reports whether the control request asks for a tool decision.
func (r *controlRequest) isPermissionRequest() bool {
	return r.Subtype == permissionSubtypePrimary || r.Subtype == permissionSubtypeFallback
}

// parseSessionEvent decodes one stdout line. An unparseable line fails the turn
// loudly and records only the line's shape — its byte length — never its content.
func parseSessionEvent(ctx context.Context, line []byte) (sessionEvent, error) {
	var event sessionEvent
	if err := json.Unmarshal(line, &event); err != nil {
		return sessionEvent{}, errors.Errorf(
			ctx,
			"unparseable event from claude process (%d bytes)",
			len(line),
		)
	}
	return event, nil
}

// sessionTurn is one input turn in the shape the CLI's stream-json input mode reads.
type sessionTurn struct {
	Type    string             `json:"type"`
	Message sessionTurnMessage `json:"message"`
}

type sessionTurnMessage struct {
	Role    string               `json:"role"`
	Content []sessionTurnContent `json:"content"`
}

type sessionTurnContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// writeTurn writes one newline-terminated JSON turn to the process's stdin. An OS
// pipe does not buffer, so no explicit flush is needed.
func writeTurn(ctx context.Context, stdin io.WriteCloser, prompt string) error {
	payload, err := json.Marshal(sessionTurn{
		Type: "user",
		Message: sessionTurnMessage{
			Role:    "user",
			Content: []sessionTurnContent{{Type: "text", Text: prompt}},
		},
	})
	if err != nil {
		return errors.Wrap(ctx, err, "marshal turn")
	}
	if _, err := stdin.Write(append(payload, '\n')); err != nil {
		return errors.Wrap(ctx, err, "write turn to stdin")
	}
	return nil
}

// controlResponse is the answer to a control_request, in the CLI's envelope shape.
type controlResponse struct {
	Type      string                 `json:"type"`
	RequestID string                 `json:"request_id"`
	Response  controlResponsePayload `json:"response"`
}

type controlResponsePayload struct {
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Response  permissionReply `json:"response"`
}

type permissionReply struct {
	Behavior string `json:"behavior"`
	Message  string `json:"message,omitempty"`
}

// writePermissionResponse writes the verdict back to the same process's stdin,
// carrying the request id the CLI raised it with.
func writePermissionResponse(
	ctx context.Context,
	stdin io.WriteCloser,
	requestID string,
	decision PermissionDecision,
) error {
	behavior := "deny"
	if decision.Allow {
		behavior = "allow"
	}
	payload, err := json.Marshal(controlResponse{
		Type:      "control_response",
		RequestID: requestID,
		Response: controlResponsePayload{
			Subtype:   "success",
			RequestID: requestID,
			Response:  permissionReply{Behavior: behavior, Message: decision.Message},
		},
	})
	if err != nil {
		return errors.Wrap(ctx, err, "marshal permission response")
	}
	if _, err := stdin.Write(append(payload, '\n')); err != nil {
		return errors.Wrap(ctx, err, "write permission response to stdin")
	}
	return nil
}

// previewOf renders a bounded preview of a tool input. The preview is carried to
// the decider and is never logged.
func previewOf(input json.RawMessage) string {
	if len(input) > permissionPreviewMaxBytes {
		return string(input[:permissionPreviewMaxBytes])
	}
	return string(input)
}

// sessionArgs builds the argv for the long-lived process. It is the single place
// the stream-json flag set lives, so a protocol correction is a one-line change.
//
// The argv does not depend on the session id: continuity comes from the held
// process, so the id is never interpolated into a command line and cannot inject a
// flag or shell syntax.
func sessionArgs(config ClaudeRunnerConfig, decider PermissionDecider) []string {
	args := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--strict-mcp-config",
	}
	if decider != nil {
		args = append(args, "--permission-prompt-tool", "stdio")
	}
	if len(config.AllowedTools) > 0 {
		args = append(args, "--allowedTools", config.AllowedTools.String())
	}
	if config.Model != "" {
		args = append(args, "--model", config.Model.String())
	}
	return args
}

// startProcess spawns the session's long-lived process. It is the only function in
// this file that creates a process, which is what makes "the same process across
// two turns" true by construction: every other path can only reuse this one.
//
// The process is deliberately not bound to ctx: ctx is the caller's request scope,
// and a process tied to it would die when the first request returned.
func startProcess(
	ctx context.Context,
	config ClaudeRunnerConfig,
	decider PermissionDecider,
) (*exec.Cmd, io.WriteCloser, *bufio.Scanner, error) {
	args := sessionArgs(config, decider)
	cmd := exec.Command("claude", args...)

	if config.WorkingDirectory != "" {
		workDir, err := config.WorkingDirectory.Resolve(ctx)
		if err != nil {
			return nil, nil, nil, errors.Wrap(ctx, err, "resolve WorkingDirectory")
		}
		cmd.Dir = workDir
	}

	env, err := buildSubprocessEnv(ctx, config)
	if err != nil {
		return nil, nil, nil, errors.Wrap(ctx, err, "build subprocess env")
	}
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, errors.Wrap(ctx, err, "create stdin pipe")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, errors.Wrap(ctx, err, "create stdout pipe")
	}

	glog.V(2).Infof("starting claude session process: claude %v", args)
	if err := cmd.Start(); err != nil {
		// Start does not close pipes that were created before it failed, so they are
		// released here rather than leaked for the process's lifetime.
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, nil, nil, errors.Wrap(ctx, err, "start claude process")
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, sessionEventBufferSize), sessionMaxEventSize)
	return cmd, stdin, scanner, nil
}
