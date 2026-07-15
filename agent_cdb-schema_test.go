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
// name string literals produced by TaskV1SchemaID. It exists to prove that
// migrating from cqrs v0.5.2 (base.Branch) to cqrs v0.6.0 (base.TopicPrefix)
// does not rename any existing quant/prod topic.
//
// Do NOT change the "dev"/"prod" expected literals here — they were frozen
// against cqrs v0.5.2 before this migration and must stay byte-identical.
// Only the call sites (how the prefix is passed in) may change.
var _ = Describe("TaskV1SchemaID topic names (golden master)", func() {
	Describe("dev branch (via base.TopicPrefixFromBranch)", func() {
		It("produces the frozen dev topic names", func() {
			prefix := base.TopicPrefixFromBranch(base.Branch("dev"))
			Expect(
				lib.TaskV1SchemaID.ResultTopic(prefix).String(),
			).To(Equal("develop-agent-task-v1-result"))
			Expect(
				lib.TaskV1SchemaID.CommandTopic(prefix).String(),
			).To(Equal("develop-agent-task-v1-request"))
			Expect(
				lib.TaskV1SchemaID.EventTopic(prefix).String(),
			).To(Equal("develop-agent-task-v1-event"))
			Expect(
				lib.TaskV1SchemaID.HistoryTopic(prefix).String(),
			).To(Equal("develop-agent-task-v1-history"))
		})
	})

	Describe("prod branch (via base.TopicPrefixFromBranch)", func() {
		It("produces the frozen prod topic names", func() {
			prefix := base.TopicPrefixFromBranch(base.Branch("prod"))
			Expect(
				lib.TaskV1SchemaID.ResultTopic(prefix).String(),
			).To(Equal("master-agent-task-v1-result"))
			Expect(
				lib.TaskV1SchemaID.CommandTopic(prefix).String(),
			).To(Equal("master-agent-task-v1-request"))
			Expect(
				lib.TaskV1SchemaID.EventTopic(prefix).String(),
			).To(Equal("master-agent-task-v1-event"))
			Expect(
				lib.TaskV1SchemaID.HistoryTopic(prefix).String(),
			).To(Equal("master-agent-task-v1-history"))
		})
	})

	Describe("empty prefix", func() {
		It("produces unprefixed topic names (Octopus behavior)", func() {
			prefix := base.TopicPrefix("")
			Expect(
				lib.TaskV1SchemaID.ResultTopic(prefix).String(),
			).To(Equal("agent-task-v1-result"))
			Expect(
				lib.TaskV1SchemaID.CommandTopic(prefix).String(),
			).To(Equal("agent-task-v1-request"))
			Expect(lib.TaskV1SchemaID.EventTopic(prefix).String()).To(Equal("agent-task-v1-event"))
			Expect(
				lib.TaskV1SchemaID.HistoryTopic(prefix).String(),
			).To(Equal("agent-task-v1-history"))
		})
	})
})
