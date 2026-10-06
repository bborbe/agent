// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive_test

import (
	"net/http"
	"time"

	libhttp "github.com/bborbe/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/interactive"
)

var _ = Describe("server write deadline", func() {
	It("raises the write deadline to ten minutes, above the library default", func() {
		server := libhttp.CreateHTTPServer(
			":0",
			http.NotFoundHandler(),
			libhttp.CreateServerOptions(interactive.ServerOptionFns()...),
		)

		// The duration is asserted as a literal on purpose: an assertion against an
		// exported alias would move with the constant and lock nothing.
		Expect(server.WriteTimeout).To(Equal(10 * time.Minute))
		Expect(server.WriteTimeout).To(BeNumerically(">", 30*time.Second))
	})
})
