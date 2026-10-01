// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pi

import (
	"context"

	"github.com/bborbe/errors"

	agentlib "github.com/bborbe/agent"
)

// NewSession wraps runner as a session: one conversation whose turns are runs.
//
// A pi session holds nothing between turns — each turn spawns its own process, and
// continuity lives in the pi CLI's own session store, keyed by the --session-id the
// runner was configured with. The session therefore adds no state of its own; it is
// the adapter that lets a Runner be addressed the way the shared interactive service
// addresses a conversation.
func NewSession(runner Runner) agentlib.Session {
	return &session{runner: runner}
}

type session struct {
	runner Runner
}

// Prompt runs one turn through the runner and returns that turn's answer as plain
// text.
func (s *session) Prompt(ctx context.Context, prompt string) (string, error) {
	result, err := s.runner.Run(ctx, prompt)
	if err != nil {
		return "", errors.Wrap(ctx, err, "run prompt")
	}
	return result.GetResult(), nil
}

// Close releases the session. There is nothing to release: pi spawns one process per
// turn and exits it before Run returns, so no resource outlives a turn. The method
// exists because the session interface requires it and a backend that holds a
// process needs it.
func (s *session) Close(_ context.Context) error {
	return nil
}

// NewSessionFactory returns a SessionFactory that builds one pi-backed session per
// session id.
//
// base carries the agent's own configuration — AgentDir, AllowedTools, Model, Env —
// and is copied, never mutated. newRunner constructs each session's runner from the
// config this factory derives: pass NewRunner in production; a test passes a recorder
// so it can observe the config each path produces.
//
// The derived config is base with the continuity pair set from id. A non-empty id
// turns persistence on and pins the run to it, which is what makes a second prompt on
// the same id continue the first. An empty id leaves both unset, so a caller with no
// session identity keeps exactly the runner it had before — the task-routed shape,
// unchanged.
func NewSessionFactory(
	base PiRunnerConfig,
	newRunner func(PiRunnerConfig) Runner,
) agentlib.SessionFactory {
	return &sessionFactory{base: base, newRunner: newRunner}
}

type sessionFactory struct {
	base      PiRunnerConfig
	newRunner func(PiRunnerConfig) Runner
}

func (f *sessionFactory) Create(id string) agentlib.Session {
	config := f.base
	config.PersistSession = id != ""
	config.SessionID = id
	return NewSession(f.newRunner(config))
}
