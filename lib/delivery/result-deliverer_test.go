// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery_test

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib/delivery"
	libmocks "github.com/bborbe/agent/lib/delivery/mocks"
)

var _ = Describe("NoopResultDeliverer", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("returns nil for done result", func() {
		deliverer := delivery.NewNoopResultDeliverer()
		err := deliverer.DeliverResult(
			ctx,
			delivery.AgentResultInfo{Status: delivery.AgentStatusDone},
		)
		Expect(err).NotTo(HaveOccurred())
	})

	It("returns nil for failed result", func() {
		deliverer := delivery.NewNoopResultDeliverer()
		err := deliverer.DeliverResult(
			ctx,
			delivery.AgentResultInfo{Status: delivery.AgentStatusFailed},
		)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("FileResultDeliverer", func() {
	var (
		ctx       context.Context
		generator *libmocks.AgentContentGenerator
		tmpFile   *os.File
		deliverer delivery.ResultDeliverer
	)

	BeforeEach(func() {
		ctx = context.Background()
		generator = &libmocks.AgentContentGenerator{}
		var err error
		tmpFile, err = os.CreateTemp("", "task-*.md")
		Expect(err).NotTo(HaveOccurred())
		Expect(
			os.WriteFile(tmpFile.Name(), []byte("---\ntitle: Test\n---\n\nBody.\n"), 0600),
		).To(Succeed())
		deliverer = delivery.NewFileResultDeliverer(generator, tmpFile.Name())
	})

	AfterEach(func() {
		Expect(os.Remove(tmpFile.Name())).To(Succeed())
	})

	It("calls generator with file content and writes generated result to disk", func() {
		generated := "---\ntitle: Test\nstatus: completed\n---\n\nBody.\n\n## Result\n\nbt-123\n"
		generator.GenerateReturns(generated, nil)
		err := deliverer.DeliverResult(
			ctx,
			delivery.AgentResultInfo{Status: delivery.AgentStatusDone, Output: "bt-123"},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(generator.GenerateCallCount()).To(Equal(1))
		written, err := os.ReadFile(tmpFile.Name())
		Expect(err).NotTo(HaveOccurred())
		Expect(string(written)).To(Equal(generated))
	})

	It("returns error when file does not exist", func() {
		deliverer = delivery.NewFileResultDeliverer(generator, "/nonexistent/path/task.md")
		err := deliverer.DeliverResult(
			ctx,
			delivery.AgentResultInfo{Status: delivery.AgentStatusDone},
		)
		Expect(err).To(HaveOccurred())
	})
})
