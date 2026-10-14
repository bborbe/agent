// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/claude"
)

var _ = Describe("PermissionSource", func() {
	It("round-trips the /prompt source through the context", func() {
		ctx := claude.WithPermissionSource(context.Background(), claude.PromptPermissionSource)
		Expect(claude.PermissionSourceFromContext(ctx)).To(Equal(claude.PromptPermissionSource))
	})

	It("round-trips the /a2a source through the context", func() {
		ctx := claude.WithPermissionSource(context.Background(), claude.A2APermissionSource)
		Expect(claude.PermissionSourceFromContext(ctx)).To(Equal(claude.A2APermissionSource))
	})

	It("yields the empty source when the context carries none", func() {
		Expect(claude.PermissionSourceFromContext(context.Background())).To(
			Equal(claude.PermissionSource("")),
		)
	})

	It("names two distinct, non-empty sources", func() {
		Expect(claude.PromptPermissionSource).NotTo(BeEmpty())
		Expect(claude.A2APermissionSource).NotTo(BeEmpty())
		Expect(claude.PromptPermissionSource).NotTo(Equal(claude.A2APermissionSource))
	})
})
