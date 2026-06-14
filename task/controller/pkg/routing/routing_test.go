// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package routing_test

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"

	lib "github.com/bborbe/agent/lib"
	task "github.com/bborbe/agent/lib/command/task"
	"github.com/bborbe/agent/task/controller/pkg/routing"
)

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -generate

func TestSuite(t *testing.T) {
	time.Local = time.UTC
	format.TruncatedDiff = false
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.Timeout = 60 * time.Second
	RunSpecs(t, "Test Suite", suiteConfig, reporterConfig)
}

var _ = Describe("ShouldProcess", func() {
	DescribeTable("routing matrix",
		func(cmdTargetVault, myVault string, want bool) {
			cmd := task.CreateCommand{
				TaskIdentifier: lib.TaskIdentifier("task-1"),
				Title:          "T",
				Frontmatter:    lib.TaskFrontmatter{"status": "next"},
				TargetVault:    cmdTargetVault,
			}
			Expect(routing.ShouldProcess(cmd, myVault)).To(Equal(want))
		},
		// (cmd empty, my openclaw) → true (legacy fallback to openclaw)
		Entry("empty target, myVault=openclaw → true (legacy fallback)", "", "openclaw", true),
		// (cmd openclaw, my openclaw) → true
		Entry("openclaw target, myVault=openclaw → true", "openclaw", "openclaw", true),
		// (cmd personal, my personal) → true
		Entry("personal target, myVault=personal → true", "personal", "personal", true),
		// (cmd empty, my personal) → false (legacy fallback is openclaw, not personal)
		Entry("empty target, myVault=personal → false (legacy is openclaw)", "", "personal", false),
		// (cmd openclaw, my personal) → false
		Entry("openclaw target, myVault=personal → false", "openclaw", "personal", false),
		// (cmd other, my openclaw) → false
		Entry("other target, myVault=openclaw → false", "other", "openclaw", false),
	)
})

var _ = Describe("ValidateMyVault", func() {
	var ctx context.Context
	BeforeEach(func() { ctx = context.Background() })

	It("rejects empty MY_VAULT", func() {
		err := routing.ValidateMyVault(ctx, "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("MY_VAULT"))
	})

	It("rejects invalid slug 'Bad'", func() {
		err := routing.ValidateMyVault(ctx, "Bad")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("MY_VAULT"))
		Expect(err.Error()).To(ContainSubstring("^[a-z][a-z0-9-]*$"))
	})

	It("accepts openclaw", func() {
		Expect(routing.ValidateMyVault(ctx, "openclaw")).To(Succeed())
	})

	It("accepts personal", func() {
		Expect(routing.ValidateMyVault(ctx, "personal")).To(Succeed())
	})
})
