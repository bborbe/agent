// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package envparse_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/envparse"
)

var _ = Describe("KeyValuePairs", func() {
	It("returns nil for empty input", func() {
		Expect(envparse.KeyValuePairs("")).To(BeNil())
	})

	It("parses a single pair", func() {
		Expect(envparse.KeyValuePairs("FOO=bar")).To(Equal(map[string]string{"FOO": "bar"}))
	})

	It("parses multiple pairs", func() {
		Expect(envparse.KeyValuePairs("A=1,B=2")).To(Equal(map[string]string{"A": "1", "B": "2"}))
	})

	It("trims whitespace around each pair", func() {
		Expect(
			envparse.KeyValuePairs(" A=1 , B=2 "),
		).To(Equal(map[string]string{"A": "1", "B": "2"}))
	})

	It("trims whitespace around keys and values separately", func() {
		Expect(
			envparse.KeyValuePairs(" A = 1 , B = 2 "),
		).To(Equal(map[string]string{"A": "1", "B": "2"}))
	})

	It("keeps the value verbatim when it contains '='", func() {
		Expect(envparse.KeyValuePairs("EQ=a=b=c")).To(Equal(map[string]string{"EQ": "a=b=c"}))
	})

	It("skips entries without '='", func() {
		Expect(envparse.KeyValuePairs("OK=yes,SKIPME")).To(Equal(map[string]string{"OK": "yes"}))
	})

	It("returns an empty map when nothing parses", func() {
		Expect(envparse.KeyValuePairs("NOEQ")).To(Equal(map[string]string{}))
	})
})
