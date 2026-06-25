// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/claude"
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

var _ = Describe("claudeRunner CLAUDE_CONFIG_DIR env propagation", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	// writeEnvShim writes a fake "claude" binary that echoes the named env var
	// as the `result` field of a stream-json result event, then exits 0.
	writeEnvShim := func(envVar string) {
		shimDir := GinkgoT().TempDir()
		shimPath := filepath.Join(shimDir, "claude")
		script := fmt.Sprintf(`#!/bin/sh
echo "{\"type\":\"result\",\"result\":\"%s=$%s\"}"
exit 0
`, envVar, envVar)
		Expect(os.WriteFile(shimPath, []byte(script), 0755)).To(Succeed()) //nolint:gosec
		originalPath := os.Getenv("PATH")
		DeferCleanup(func() {
			Expect(os.Setenv("PATH", originalPath)).To(Succeed())
		})
		Expect(os.Setenv("PATH", shimDir+":"+originalPath)).To(Succeed())
	}

	Context("when config.ClaudeConfigDir is empty (default)", func() {
		BeforeEach(func() {
			writeEnvShim("CLAUDE_CONFIG_DIR")
		})

		It("passes CLAUDE_CONFIG_DIR=<expanded ~/.claude> to the subprocess", func() {
			home, err := os.UserHomeDir()
			Expect(err).NotTo(HaveOccurred())

			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Result).To(Equal("CLAUDE_CONFIG_DIR=" + filepath.Join(home, ".claude")))
		})
	})

	Context("when config.ClaudeConfigDir is an explicit absolute path", func() {
		BeforeEach(func() {
			writeEnvShim("CLAUDE_CONFIG_DIR")
		})

		It("passes that path through unchanged", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
				ClaudeConfigDir: claude.ClaudeConfigDir("/custom/claude/path"),
			}).Run(ctx, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Result).To(Equal("CLAUDE_CONFIG_DIR=/custom/claude/path"))
		})
	})

	Context("when config.ClaudeConfigDir is a tilde-prefixed path", func() {
		BeforeEach(func() {
			writeEnvShim("CLAUDE_CONFIG_DIR")
		})

		It("expands the tilde to the user's home directory", func() {
			home, err := os.UserHomeDir()
			Expect(err).NotTo(HaveOccurred())

			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
				ClaudeConfigDir: claude.ClaudeConfigDir("~/custom-claude"),
			}).Run(ctx, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(
				result.Result,
			).To(Equal("CLAUDE_CONFIG_DIR=" + filepath.Join(home, "custom-claude")))
		})
	})

	Context("when CLAUDE_CONFIG_DIR is set in the parent process env", func() {
		BeforeEach(func() {
			writeEnvShim("CLAUDE_CONFIG_DIR")
			originalEnv, hadOriginal := os.LookupEnv("CLAUDE_CONFIG_DIR")
			DeferCleanup(func() {
				if hadOriginal {
					Expect(os.Setenv("CLAUDE_CONFIG_DIR", originalEnv)).To(Succeed())
				} else {
					Expect(os.Unsetenv("CLAUDE_CONFIG_DIR")).To(Succeed())
				}
			})
			Expect(os.Setenv("CLAUDE_CONFIG_DIR", "/env-set/claude")).To(Succeed())
		})

		It("uses the parent env value when explicit config is empty", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Result).To(Equal("CLAUDE_CONFIG_DIR=/env-set/claude"))
		})

		It("explicit config takes precedence over parent env", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
				ClaudeConfigDir: claude.ClaudeConfigDir("/explicit/claude"),
			}).Run(ctx, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Result).To(Equal("CLAUDE_CONFIG_DIR=/explicit/claude"))
		})
	})

	Context("when r.config.Env explicitly sets CLAUDE_CONFIG_DIR", func() {
		BeforeEach(func() {
			writeEnvShim("CLAUDE_CONFIG_DIR")
		})

		It("the consumer-provided value wins over everything (highest precedence)", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
				ClaudeConfigDir: claude.ClaudeConfigDir("/explicit/claude"),
				Env: map[string]string{
					"CLAUDE_CONFIG_DIR": "/consumer-env/claude",
				},
			}).Run(ctx, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Result).To(Equal("CLAUDE_CONFIG_DIR=/consumer-env/claude"))
		})
	})
})

var _ = Describe("claudeRunner AllowedTools buildCommand branch", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

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

	Context("with AllowedTools configured (buildCommand --allowedTools branch)", func() {
		BeforeEach(func() {
			writeShim(
				`echo "{\"type\":\"result\",\"result\":\"Args: $@\"}"
exit 0`,
			)
		})

		It("passes --allowedTools flag with the tool list", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
				AllowedTools: claude.ParseAllowedTools("Read,Write,Bash"),
			}).Run(ctx, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Result).To(ContainSubstring("--allowedTools"))
			Expect(result.Result).To(ContainSubstring("Read,Write,Bash"))
		})
	})

	Context("with Model configured (buildCommand --model branch)", func() {
		BeforeEach(func() {
			writeShim(
				`echo "{\"type\":\"result\",\"result\":\"Args: $@\"}"
exit 0`,
			)
		})

		It("passes --model flag with the model name", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
				Model: claude.OpusClaudeModel,
			}).Run(ctx, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Result).To(ContainSubstring("--model"))
			Expect(result.Result).To(ContainSubstring("opus"))
		})
	})

	Context("with both AllowedTools and Model configured", func() {
		BeforeEach(func() {
			writeShim(
				`echo "{\"type\":\"result\",\"result\":\"Args: $@\"}"
exit 0`,
			)
		})

		It("passes both --allowedTools and --model flags", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
				AllowedTools: claude.ParseAllowedTools("Read"),
				Model:        claude.SonnetClaudeModel,
			}).Run(ctx, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Result).To(ContainSubstring("--allowedTools"))
			Expect(result.Result).To(ContainSubstring("--model"))
		})
	})

	Context("with WorkingDirectory configured (buildCommand cmd.Dir branch)", func() {
		var workDir string

		BeforeEach(func() {
			var err error
			// Resolve symlinks so the test works on macOS where /var → /private/var.
			workDir, err = filepath.EvalSymlinks(GinkgoT().TempDir())
			Expect(err).NotTo(HaveOccurred())
			writeShim(
				`echo "{\"type\":\"result\",\"result\":\"PWD=$PWD\"}"
exit 0`,
			)
		})

		It("sets cmd.Dir to the working directory", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
				WorkingDirectory: claude.AgentDir(workDir),
			}).Run(ctx, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Result).To(ContainSubstring("PWD=" + workDir))
		})
	})
})
