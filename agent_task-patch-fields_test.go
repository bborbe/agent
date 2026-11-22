// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"regexp"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent"
)

var _ = Describe("TaskPatchFields", func() {
	fieldNamePattern := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	It("unions the typed and pipeline sets", func() {
		typed := lib.TaskTypedFields()
		pipeline := lib.TaskPipelineFields()
		patch := lib.TaskPatchFields()

		for key := range typed {
			Expect(patch).To(HaveKey(key), "typed key %q missing from patch set", key)
		}
		for key := range pipeline {
			Expect(patch).To(HaveKey(key), "pipeline key %q missing from patch set", key)
		}
		Expect(patch).To(HaveLen(len(typed) + len(pipeline)))
	})

	It("keeps the typed and pipeline sets disjoint", func() {
		typed := lib.TaskTypedFields()
		pipeline := lib.TaskPipelineFields()

		for key := range typed {
			Expect(pipeline).NotTo(HaveKey(key), "key %q is in both sets", key)
		}
		for key := range pipeline {
			Expect(typed).NotTo(HaveKey(key), "key %q is in both sets", key)
		}
	})

	It("uses non-empty snake_case keys", func() {
		for key := range lib.TaskPatchFields() {
			Expect(key).NotTo(BeEmpty())
			Expect(key).To(MatchRegexp(fieldNamePattern.String()), "key %q", key)
		}
	})

	It("has the expected size for each set", func() {
		Expect(lib.TaskTypedFields()).To(HaveLen(20))
		Expect(lib.TaskPipelineFields()).To(HaveLen(47))
		Expect(lib.TaskPatchFields()).To(HaveLen(67))
	})

	It("returns an independent copy from TaskTypedFields", func() {
		fields := lib.TaskTypedFields()
		fields["status"] = false
		delete(fields, "phase")

		fresh := lib.TaskTypedFields()
		Expect(fresh).To(HaveKey("status"))
		Expect(fresh["status"]).To(BeTrue())
		Expect(fresh).To(HaveKey("phase"))
	})

	It("returns an independent copy from TaskPipelineFields", func() {
		fields := lib.TaskPipelineFields()
		fields["max_retries"] = false
		delete(fields, "title")

		fresh := lib.TaskPipelineFields()
		Expect(fresh).To(HaveKey("max_retries"))
		Expect(fresh["max_retries"]).To(BeTrue())
		Expect(fresh).To(HaveKey("title"))
	})

	It("returns an independent copy from TaskPatchFields", func() {
		fields := lib.TaskPatchFields()
		fields["status"] = false
		delete(fields, "max_retries")

		fresh := lib.TaskPatchFields()
		Expect(fresh).To(HaveKey("status"))
		Expect(fresh["status"]).To(BeTrue())
		Expect(fresh).To(HaveKey("max_retries"))
	})

	Describe("TaskPatchFieldAdmitted", func() {
		It("admits every enumerated key", func() {
			for key := range lib.TaskPatchFields() {
				Expect(lib.TaskPatchFieldAdmitted(key)).To(BeTrue(), "key %q", key)
			}
		})

		It("admits a synthetic future metrics key", func() {
			Expect(lib.TaskPatchFieldAdmitted("metrics_not_yet_invented")).To(BeTrue())
		})

		It("rejects an unknown non-namespace key", func() {
			Expect(lib.TaskPatchFieldAdmitted("not_a_task_field")).To(BeFalse())
		})

		It(
			"run_claim is a spawn-gate claim marker written at execution start, whose only expiry is its TTL, so a result write must never be able to set it",
			func() {
				Expect(lib.TaskPatchFieldAdmitted("run_claim")).To(BeFalse())
				Expect(lib.TaskPatchFields()).NotTo(HaveKey("run_claim"))
			},
		)
	})
})
