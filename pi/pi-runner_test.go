// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pi_test

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/pi"
)

var _ = Describe("piRunner cwd", func() {
	var (
		ctx          context.Context
		shimDir      string
		originalPath string
	)

	BeforeEach(func() {
		ctx = context.Background()
		shimDir = GinkgoT().TempDir()
		shimPath := filepath.Join(shimDir, "pi")
		// Shim emits JSON in the format pi --mode json produces.
		// "$PWD" lets us assert the working directory pi was spawned in.
		script := `#!/bin/sh
printf '{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"CWD=%s"}]}]}\n' "$PWD"
`
		Expect(os.WriteFile(shimPath, []byte(script), 0755)).To(Succeed()) //nolint:gosec
		originalPath = os.Getenv("PATH")
		Expect(os.Setenv("PATH", shimDir+":"+originalPath)).To(Succeed())
		DeferCleanup(func() {
			Expect(os.Setenv("PATH", originalPath)).To(Succeed())
		})
	})

	It("spawns pi with cwd = AgentDir when AgentDir is set", func() {
		workDir, err := filepath.EvalSymlinks(GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		runner := pi.NewRunner(pi.PiRunnerConfig{AgentDir: workDir})
		result, err := runner.Run(ctx, "test")
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Result).To(ContainSubstring("CWD=" + workDir))
	})

	It("inherits parent cwd when AgentDir is empty", func() {
		parentCwd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		parentCwd, err = filepath.EvalSymlinks(parentCwd)
		Expect(err).NotTo(HaveOccurred())
		runner := pi.NewRunner(pi.PiRunnerConfig{})
		result, err := runner.Run(ctx, "test")
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Result).To(ContainSubstring("CWD=" + parentCwd))
	})
})

var _ = Describe("piRunner session flag", func() {
	var (
		ctx          context.Context
		shimDir      string
		originalPath string
	)

	BeforeEach(func() {
		ctx = context.Background()
		shimDir = GinkgoT().TempDir()
		shimPath := filepath.Join(shimDir, "pi")
		// Shim echoes its own arguments so the spec can assert which flags were passed.
		script := `#!/bin/sh
printf '{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"ARGS=%s"}]}]}\n' "$*"
`
		Expect(os.WriteFile(shimPath, []byte(script), 0755)).To(Succeed()) //nolint:gosec
		originalPath = os.Getenv("PATH")
		Expect(os.Setenv("PATH", shimDir+":"+originalPath)).To(Succeed())
		DeferCleanup(func() {
			Expect(os.Setenv("PATH", originalPath)).To(Succeed())
		})
	})

	It("passes --no-session by default, so a task-routed run leaves no session behind", func() {
		runner := pi.NewRunner(pi.PiRunnerConfig{})
		result, err := runner.Run(ctx, "test")
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Result).To(ContainSubstring("--no-session"))
	})

	It("omits --no-session when PersistSession is set", func() {
		runner := pi.NewRunner(pi.PiRunnerConfig{PersistSession: true})
		result, err := runner.Run(ctx, "test")
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Result).NotTo(ContainSubstring("--no-session"))
	})

	It("passes --session-id when one is configured", func() {
		// Persisting alone does not give continuity: --no-session governs whether pi
		// *writes* the transcript, and --session-id is what reads it back.
		runner := pi.NewRunner(pi.PiRunnerConfig{PersistSession: true, SessionID: "identity"})
		result, err := runner.Run(ctx, "test")
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Result).To(ContainSubstring("--session-id identity"))
	})

	It("passes no --session-id when none is configured", func() {
		// A task-routed run must not be pinned to one identity: each run is an
		// unrelated task, and a shared session id would join them.
		runner := pi.NewRunner(pi.PiRunnerConfig{})
		result, err := runner.Run(ctx, "test")
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Result).NotTo(ContainSubstring("--session-id"))
	})
})

var _ = Describe("piRunner event vocabulary", func() {
	var (
		ctx          context.Context
		shimDir      string
		originalPath string
	)

	// Each spec installs a shim emitting one vocabulary's worth of output. The
	// runner has to read the answer out of all of them: pi is installed unpinned,
	// so its event names have already changed once and can change again.
	setShim := func(script string) {
		shimPath := filepath.Join(shimDir, "pi")
		Expect(os.WriteFile(shimPath, []byte(script), 0755)).To(Succeed()) //nolint:gosec
	}

	BeforeEach(func() {
		ctx = context.Background()
		shimDir = GinkgoT().TempDir()
		originalPath = os.Getenv("PATH")
		Expect(os.Setenv("PATH", shimDir+":"+originalPath)).To(Succeed())
		DeferCleanup(func() {
			Expect(os.Setenv("PATH", originalPath)).To(Succeed())
		})
	})

	It("reads the answer from the older agent_end vocabulary", func() {
		setShim(`#!/bin/sh
printf '{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"OLD"}]}]}\n'
`)

		result, err := pi.NewRunner(pi.PiRunnerConfig{}).Run(ctx, "test")

		Expect(err).NotTo(HaveOccurred())
		Expect(result.Result).To(Equal("OLD"))
	})

	It("reads the answer from the message_end vocabulary pi 0.87.x emits", func() {
		// Observed live on 2026-09-26: a v0.4.0 agent-pi pod produced exactly this
		// stream and the runner answered "no result found in pi CLI output" — on a
		// run that had in fact answered correctly. The symptom points at the model;
		// the defect is here.
		setShim(`#!/bin/sh
printf '{"type":"session","version":3}\n'
printf '{"type":"message_end","message":{"role":"system","content":[{"type":"text","text":"SYSTEM"}]}}\n'
printf '{"type":"message_end","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"NEW"}]}}\n'
printf '{"type":"agent_settled"}\n'
`)

		result, err := pi.NewRunner(pi.PiRunnerConfig{}).Run(ctx, "test")

		Expect(err).NotTo(HaveOccurred())
		Expect(result.Result).To(Equal("NEW"))
	})

	It("does not return a non-assistant message as the answer", func() {
		// The role guard is the whole point: message_end fires for every role, so
		// without it a run producing no assistant text would echo the system prompt
		// back as a successful result — the worst kind of failure, since it looks
		// like an answer.
		setShim(`#!/bin/sh
printf '{"type":"message_end","message":{"role":"system","content":[{"type":"text","text":"SYSTEM PROMPT"}]}}\n'
`)

		_, err := pi.NewRunner(pi.PiRunnerConfig{}).Run(ctx, "test")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no result found"))
	})
})
