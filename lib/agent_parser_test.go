// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/lib/mocks"
)

type TestPlan struct {
	Action string `json:"action"`
}

var _ = Describe("ParseStep", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Run", func() {
		It("parses task content and replaces section", func() {
			parser := &mocks.AgentAIParser{}
			parser.ParseReturns(nil)

			step := lib.NewParseStep[TestPlan]("parse-plan", parser, "## Plan", "in_progress")

			md := &lib.Markdown{
				Sections: []lib.Section{
					{Heading: "## Plan", Body: "old content"},
				},
			}

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(lib.AgentStatusDone))
			Expect(result.NextPhase).To(Equal("in_progress"))

			section, found := md.FindSection("## Plan")
			Expect(found).To(BeTrue())
			Expect(section.Body).To(ContainSubstring("```json"))
		})

		It("returns NeedsInput when parser fails", func() {
			parser := &mocks.AgentAIParser{}
			parser.ParseReturns(errors.New("parse error"))

			step := lib.NewParseStep[TestPlan]("parse-plan", parser, "## Plan", "in_progress")

			md := &lib.Markdown{}

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(lib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("parse error"))
		})
	})

	Describe("ShouldRun", func() {
		It("returns true when section absent", func() {
			parser := &mocks.AgentAIParser{}
			step := lib.NewParseStep[TestPlan]("parse-plan", parser, "## Plan", "in_progress")

			md := &lib.Markdown{
				Sections: []lib.Section{
					{Heading: "## Other", Body: "content"},
				},
			}

			shouldRun, err := step.ShouldRun(ctx, md)
			Expect(err).To(BeNil())
			Expect(shouldRun).To(BeTrue())
		})

		It("returns false when section exists", func() {
			parser := &mocks.AgentAIParser{}
			step := lib.NewParseStep[TestPlan]("parse-plan", parser, "## Plan", "in_progress")

			md := &lib.Markdown{
				Sections: []lib.Section{
					{Heading: "## Plan", Body: "already exists"},
				},
			}

			shouldRun, err := step.ShouldRun(ctx, md)
			Expect(err).To(BeNil())
			Expect(shouldRun).To(BeFalse())
		})
	})
})
