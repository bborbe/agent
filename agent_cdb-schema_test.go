// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"github.com/bborbe/cqrs/base"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent"
)

// This is a golden-master characterization test. It pins the exact Kafka topic
// name string literals produced by TaskV1SchemaID today (on cqrs v0.5.2, keyed
// by base.Branch). It exists to prove that migrating to cqrs v0.6.0's
// base.TopicPrefix does not rename any existing quant/prod topic.
//
// Do NOT change the expected literals here when migrating to v0.6.0 — only the
// call sites (how the prefix is passed in) may change.
var _ = Describe("TaskV1SchemaID topic names (golden master)", func() {
	Describe("dev branch", func() {
		It("produces the frozen dev topic names", func() {
			Expect(
				lib.TaskV1SchemaID.ResultTopic(base.Branch("dev")).String(),
			).To(Equal("develop-agent-task-v1-result"))
			Expect(
				lib.TaskV1SchemaID.CommandTopic(base.Branch("dev")).String(),
			).To(Equal("develop-agent-task-v1-request"))
			Expect(
				lib.TaskV1SchemaID.EventTopic(base.Branch("dev")).String(),
			).To(Equal("develop-agent-task-v1-event"))
			Expect(
				lib.TaskV1SchemaID.HistoryTopic(base.Branch("dev")).String(),
			).To(Equal("develop-agent-task-v1-history"))
		})
	})

	Describe("prod branch", func() {
		It("produces the frozen prod topic names", func() {
			Expect(
				lib.TaskV1SchemaID.ResultTopic(base.Branch("prod")).String(),
			).To(Equal("master-agent-task-v1-result"))
			Expect(
				lib.TaskV1SchemaID.CommandTopic(base.Branch("prod")).String(),
			).To(Equal("master-agent-task-v1-request"))
			Expect(
				lib.TaskV1SchemaID.EventTopic(base.Branch("prod")).String(),
			).To(Equal("master-agent-task-v1-event"))
			Expect(
				lib.TaskV1SchemaID.HistoryTopic(base.Branch("prod")).String(),
			).To(Equal("master-agent-task-v1-history"))
		})
	})
})
