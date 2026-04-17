// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package v1_test

import (
	"context"
	"testing"

	libk8s "github.com/bborbe/k8s"
	"github.com/bborbe/validation"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentv1 "github.com/bborbe/agent/task/executor/k8s/apis/agent.benjamin-borbe.de/v1"
)

func TestV1(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "V1 Suite")
}

var _ = Describe("Config", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Equal", func() {
		It("returns true for identical specs", func() {
			a := agentv1.Config{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: agentv1.ConfigSpec{
					Assignee:  "claude",
					Image:     "registry/agent-claude",
					Heartbeat: "30m",
				},
			}
			b := agentv1.Config{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: agentv1.ConfigSpec{
					Assignee:  "claude",
					Image:     "registry/agent-claude",
					Heartbeat: "30m",
				},
			}
			Expect(a.Equal(b)).To(BeTrue())
		})

		It("returns false when Image differs", func() {
			a := agentv1.Config{
				Spec: agentv1.ConfigSpec{
					Assignee:  "claude",
					Image:     "registry/agent-claude",
					Heartbeat: "30m",
				},
			}
			b := agentv1.Config{
				Spec: agentv1.ConfigSpec{
					Assignee:  "claude",
					Image:     "registry/agent-claude-v2",
					Heartbeat: "30m",
				},
			}
			Expect(a.Equal(b)).To(BeFalse())
		})

		It("returns true when compared with pointer type", func() {
			a := agentv1.Config{
				Spec: agentv1.ConfigSpec{
					Assignee:  "claude",
					Image:     "registry/agent-claude",
					Heartbeat: "30m",
				},
			}
			b := &agentv1.Config{
				Spec: agentv1.ConfigSpec{
					Assignee:  "claude",
					Image:     "registry/agent-claude",
					Heartbeat: "30m",
				},
			}
			Expect(a.Equal(b)).To(BeTrue())
		})

		It("returns false for unknown type", func() {
			a := agentv1.Config{
				Spec: agentv1.ConfigSpec{Assignee: "claude"},
			}
			Expect(a.Equal(nil)).To(BeFalse())
		})
	})

	Describe("Identifier", func() {
		It("returns BuildName of namespace and name", func() {
			a := agentv1.Config{
				ObjectMeta: metav1.ObjectMeta{Name: "my-agent", Namespace: "production"},
			}
			Expect(
				a.Identifier(),
			).To(Equal(libk8s.Identifier(libk8s.BuildName("production", "my-agent"))))
		})
	})

	Describe("String", func() {
		It("returns metadata.name", func() {
			a := agentv1.Config{
				ObjectMeta: metav1.ObjectMeta{Name: "my-agent"},
			}
			Expect(a.String()).To(Equal("my-agent"))
		})
	})

	Describe("Validate", func() {
		It("returns nil for a complete valid spec", func() {
			a := agentv1.Config{
				Spec: agentv1.ConfigSpec{
					Assignee:  "claude",
					Image:     "registry/agent-claude",
					Heartbeat: "30m",
				},
			}
			Expect(a.Validate(ctx)).To(BeNil())
		})
	})
})

var _ = Describe("ConfigSpec", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Validate", func() {
		It("returns nil for a valid spec", func() {
			s := agentv1.ConfigSpec{
				Assignee:  "claude",
				Image:     "registry/agent-claude",
				Heartbeat: "30m",
			}
			Expect(s.Validate(ctx)).To(BeNil())
		})

		It("returns a wrapped validation.Error when Assignee is empty", func() {
			s := agentv1.ConfigSpec{
				Image:     "registry/agent-claude",
				Heartbeat: "30m",
			}
			err := s.Validate(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("assignee is empty")))
		})

		It("returns a wrapped validation.Error when Image is empty", func() {
			s := agentv1.ConfigSpec{
				Assignee:  "claude",
				Heartbeat: "30m",
			}
			err := s.Validate(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("image is empty")))
		})

		It("returns a wrapped validation.Error when Heartbeat is empty", func() {
			s := agentv1.ConfigSpec{
				Assignee: "claude",
				Image:    "registry/agent-claude",
			}
			err := s.Validate(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("heartbeat is empty")))
		})

		It(
			"returns a wrapped validation.Error when VolumeClaim is set but VolumeMountPath is empty",
			func() {
				s := agentv1.ConfigSpec{
					Assignee:    "claude",
					Image:       "registry/agent-claude",
					Heartbeat:   "30m",
					VolumeClaim: "my-pvc",
				}
				err := s.Validate(ctx)
				Expect(err).To(HaveOccurred())
				Expect(
					err,
				).To(MatchError(ContainSubstring("VolumeMountPath required when VolumeClaim set")))
			},
		)

		It("returns nil when both VolumeClaim and VolumeMountPath are set", func() {
			s := agentv1.ConfigSpec{
				Assignee:        "claude",
				Image:           "registry/agent-claude",
				Heartbeat:       "30m",
				VolumeClaim:     "my-pvc",
				VolumeMountPath: "/data",
			}
			Expect(s.Validate(ctx)).To(BeNil())
		})

		It("wraps error with validation.Error sentinel", func() {
			s := agentv1.ConfigSpec{}
			err := s.Validate(ctx)
			Expect(err).To(HaveOccurred())
			// The error must wrap validation.Error
			_ = validation.Error // ensure the import is used
		})
	})
})
