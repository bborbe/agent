// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task_test

import (
	"context"
	stderrors "errors"

	"github.com/bborbe/errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	task "github.com/bborbe/agent/command/task"
)

var _ = Describe("ErrTaskAlreadyExists", func() {
	It("is a non-nil sentinel with a stable message", func() {
		Expect(task.ErrTaskAlreadyExists).NotTo(BeNil())
		Expect(task.ErrTaskAlreadyExists.Error()).
			To(Equal("task file already exists at title path"))
	})

	It("remains matchable via errors.Is after wrapping with bborbe/errors.Wrapf", func() {
		wrapped := errors.Wrapf(
			context.Background(),
			task.ErrTaskAlreadyExists,
			"title path %s occupied",
			"tasks/Some Title.md",
		)
		Expect(stderrors.Is(wrapped, task.ErrTaskAlreadyExists)).To(BeTrue())
	})
})
