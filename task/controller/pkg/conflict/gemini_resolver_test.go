// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conflict_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/task/controller/pkg/conflict"
	"github.com/bborbe/agent/task/controller/pkg/gitclient"
)

var _ gitclient.ConflictResolver = &conflict.GeminiConflictResolver{}

var _ = Describe("GeminiConflictResolver", func() {
	It("implements ConflictResolver interface", func() {
		var _ gitclient.ConflictResolver = conflict.NewGeminiConflictResolver("fake-key")
		Expect(conflict.NewGeminiConflictResolver("fake-key")).NotTo(BeNil())
	})
})
