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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/claude"
)

// rateLimitRetryStorm is the stream-json tail the CLI leaves when it exhausts its
// own API retries on a 429: the last attempt carries the cap it reached.
const rateLimitRetryStorm = `echo '{"type":"system","subtype":"api_retry","error":"rate_limit","error_status":429,"attempt":10,"max_retries":10}'`

var _ = Describe("claudeRunner rate-limit retry", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	// writeShim creates a temp dir, writes a "claude" shell script with the given
	// body, prepends the dir to PATH, and registers cleanup via DeferCleanup.
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

	// invocationCount returns how many lines a counting shim appended.
	invocationCount := func(path string) int {
		content, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
		if os.IsNotExist(err) {
			return 0
		}
		Expect(err).NotTo(HaveOccurred())
		return strings.Count(string(content), "x\n")
	}

	Context("when every attempt exhausts its retries on a 429", func() {
		var countPath string

		BeforeEach(func() {
			countPath = filepath.Join(GinkgoT().TempDir(), "calls")
			writeShim(fmt.Sprintf("%s\necho x >> %s\nexit 1", rateLimitRetryStorm, countPath))
		})

		It("re-invokes up to the cap and then gives up", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
				RateLimitBackoffBase: time.Millisecond,
			}).Run(ctx, "test")

			Expect(err).To(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.RateLimitRetries).To(Equal(int64(3)))
			// One initial attempt plus three re-invokes.
			Expect(invocationCount(countPath)).To(Equal(4))
		})

		It("keeps the error text the crash taxonomy buckets on", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
				RateLimitBackoffBase: time.Millisecond,
			}).Run(ctx, "test")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("claude CLI failed:"))
			Expect(err.Error()).To(ContainSubstring("HTTP 429"))
		})
	})

	Context("when the first attempt is rate-limited and the second succeeds", func() {
		var statePath string

		BeforeEach(func() {
			statePath = filepath.Join(GinkgoT().TempDir(), "state")
			writeShim(fmt.Sprintf(
				`if [ -f %s ]; then
  echo '{"type":"result","result":"done"}'
  exit 0
fi
touch %s
%s
exit 1`,
				statePath, statePath, rateLimitRetryStorm,
			))
		})

		It("returns the successful result and counts one retry", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
				RateLimitBackoffBase: time.Millisecond,
			}).Run(ctx, "test")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Result).To(Equal("done"))
			Expect(result.RateLimitRetries).To(Equal(int64(1)))
		})
	})

	Context("when a rate-limited attempt consumed tokens before the re-invoke", func() {
		var statePath string

		BeforeEach(func() {
			statePath = filepath.Join(GinkgoT().TempDir(), "state")
			writeShim(fmt.Sprintf(
				`if [ -f %s ]; then
  echo '{"type":"result","result":"done","usage":{"input_tokens":5,"output_tokens":2},"num_turns":1}'
  exit 0
fi
touch %s
echo '{"type":"result","result":"","usage":{"input_tokens":7,"output_tokens":3},"num_turns":2}'
%s
exit 1`,
				statePath, statePath, rateLimitRetryStorm,
			))
		})

		It("sums the usage across every attempt", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
				RateLimitBackoffBase: time.Millisecond,
			}).Run(ctx, "test")

			Expect(err).NotTo(HaveOccurred())
			Expect(result.InputTokens).To(Equal(int64(12)))
			Expect(result.OutputTokens).To(Equal(int64(5)))
			Expect(result.NumTurns).To(Equal(int64(3)))
			Expect(result.RateLimitRetries).To(Equal(int64(1)))
		})
	})

	Context("when the failure is not a rate limit", func() {
		var countPath string

		BeforeEach(func() {
			countPath = filepath.Join(GinkgoT().TempDir(), "calls")
			writeShim(fmt.Sprintf(
				`echo '{"type":"error","message":"auth-failure: 401 Invalid authentication credentials"}'
echo x >> %s
exit 1`,
				countPath,
			))
		})

		It("does not re-invoke and reports zero retries", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
				RateLimitBackoffBase: time.Millisecond,
			}).Run(ctx, "test")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("auth-failure"))
			Expect(result.RateLimitRetries).To(Equal(int64(0)))
			Expect(invocationCount(countPath)).To(Equal(1))
		})
	})

	Context("when the context deadline leaves no room for the backoff", func() {
		var countPath string

		BeforeEach(func() {
			countPath = filepath.Join(GinkgoT().TempDir(), "calls")
			writeShim(fmt.Sprintf("%s\necho x >> %s\nexit 1", rateLimitRetryStorm, countPath))
		})

		It("does not start a retry it cannot finish", func() {
			// Default 15s backoff plus the 30s floor against a 10s deadline.
			shortCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(shortCtx, "test")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("claude CLI failed:"))
			Expect(result.RateLimitRetries).To(Equal(int64(0)))
			Expect(invocationCount(countPath)).To(Equal(1))
		})
	})

	Context("when the context is cancelled during the backoff", func() {
		BeforeEach(func() {
			writeShim(fmt.Sprintf("%s\nexit 1", rateLimitRetryStorm))
		})

		It("stops waiting and reports the CLI's own failure", func() {
			cancelCtx, cancel := context.WithCancel(ctx)
			go func() {
				time.Sleep(50 * time.Millisecond)
				cancel()
			}()

			done := make(chan error, 1)
			go func() {
				_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
					// Long enough that only the cancellation can end the wait.
					RateLimitBackoffBase: time.Hour,
				}).Run(cancelCtx, "test")
				done <- err
			}()

			var err error
			Eventually(done, 5*time.Second).Should(Receive(&err))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("claude CLI failed:"))
		})
	})
})
