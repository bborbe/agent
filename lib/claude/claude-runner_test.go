// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib/claude"
)

var _ = Describe("claudeRunner stdout tail", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	// writeShim creates a temp dir, writes a "claude" shell script with the given body,
	// prepends the dir to PATH, and registers cleanup via DeferCleanup.
	writeShim := func(body string) {
		shimDir := GinkgoT().TempDir()
		shimPath := filepath.Join(shimDir, "claude")
		script := "#!/bin/sh\n" + body
		err := os.WriteFile(shimPath, []byte(script), 0755) //nolint:gosec
		Expect(err).NotTo(HaveOccurred())
		originalPath := os.Getenv("PATH")
		DeferCleanup(func() {
			Expect(os.Setenv("PATH", originalPath)).To(Succeed())
		})
		Expect(os.Setenv("PATH", shimDir+":"+originalPath)).To(Succeed())
	}

	Context("non-zero exit after emitting diagnostic stdout lines", func() {
		BeforeEach(func() {
			writeShim(
				`echo '{"type":"error","message":"auth-failure: 401 Invalid authentication credentials"}'
exit 1`,
			)
		})

		It("error contains the diagnostic text the CLI emitted", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(
				err.Error(),
			).To(ContainSubstring("auth-failure: 401 Invalid authentication credentials"))
		})

		It("error does not contain the double-colon empty rendering", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).NotTo(ContainSubstring(": :"))
		})
	})

	Context("non-zero exit with no stdout output", func() {
		BeforeEach(func() {
			writeShim("exit 1")
		})

		It("error contains 'no stdout captured'", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no stdout captured"))
		})

		It("error does not contain the double-colon empty rendering", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).NotTo(ContainSubstring(": :"))
		})
	})

	Context("non-zero exit after emitting more than 5 stdout lines", func() {
		BeforeEach(func() {
			// Emits 7 lines; only the most recent 5 (lines 3–7) should appear in the error.
			writeShim(`echo 'DROPPED-line-one'
echo 'DROPPED-line-two'
echo 'retained-line-3'
echo 'retained-line-4'
echo 'retained-line-5'
echo 'retained-line-6'
echo 'retained-line-7'
exit 1`)
		})

		It("drops the two oldest lines", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).NotTo(ContainSubstring("DROPPED-line-one"))
			Expect(err.Error()).NotTo(ContainSubstring("DROPPED-line-two"))
		})

		It("retains the 5 most recent lines", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("retained-line-3"))
			Expect(err.Error()).To(ContainSubstring("retained-line-7"))
		})
	})

	Context("non-zero exit after emitting a line exceeding 512 bytes", func() {
		BeforeEach(func() {
			// Emits 600 'A' characters as a single line, then exits 1.
			writeShim("head -c 600 /dev/zero | tr '\\0' 'A'\necho\nexit 1")
		})

		It("truncates the captured line to 512 bytes", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			// 512 consecutive 'A's is the truncated content — it must appear in the error
			Expect(err.Error()).To(ContainSubstring(strings.Repeat("A", 512)))
			// 513 consecutive 'A's cannot appear — the line was truncated at 512
			Expect(err.Error()).NotTo(ContainSubstring(strings.Repeat("A", 513)))
		})
	})

	Context("zero exit with no result event in stdout", func() {
		BeforeEach(func() {
			// Exits 0 but never emits a {"type":"result"} event.
			writeShim(`echo '{"type":"system","subtype":"init"}'
exit 0`)
		})

		It("returns an error about missing result event", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no result event found"))
		})
	})

	Context("successful CLI exit", func() {
		BeforeEach(func() {
			// Emits one diagnostic line (noise) and one result event, then exits 0.
			writeShim(
				`echo '{"type":"system","subtype":"init","cwd":"/tmp","session_id":"abc","tools":[]}'
echo '{"type":"result","result":"task-output-text"}'
exit 0`,
			)
		})

		It("returns no error", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns the result from the result event", func() {
			result, _ := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(result).NotTo(BeNil())
			Expect(result.Result).To(Equal("task-output-text"))
		})
	})
})
