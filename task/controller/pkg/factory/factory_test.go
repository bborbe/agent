// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/task/controller/pkg/factory"
)

var _ = Describe("Factory", func() {
	Describe("CreateSyncLoop", func() {
		It("is defined", func() {
			Expect(factory.CreateSyncLoop).NotTo(BeNil())
		})
	})
})
