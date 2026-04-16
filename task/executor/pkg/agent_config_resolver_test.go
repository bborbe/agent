// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"context"

	"github.com/bborbe/errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/bborbe/agent/task/executor/k8s/apis/agents.bborbe.dev/v1"
	pkg "github.com/bborbe/agent/task/executor/pkg"
)

// fakeProvider is a simple in-memory Provider[v1.AgentConfig] for tests.
type fakeProvider struct {
	items []v1.AgentConfig
	err   error
}

func (f *fakeProvider) Get(_ context.Context) ([]v1.AgentConfig, error) {
	return f.items, f.err
}

var _ = Describe("AgentConfigResolver", func() {
	var (
		ctx      context.Context
		provider *fakeProvider
		resolver pkg.AgentConfigResolver
	)

	BeforeEach(func() {
		ctx = context.Background()
		provider = &fakeProvider{}
		resolver = pkg.NewAgentConfigResolver(provider, "dev")
	})

	It("returns converted AgentConfiguration with image tag appended", func() {
		provider.items = []v1.AgentConfig{
			{
				Spec: v1.AgentConfigSpec{
					Assignee:        "claude",
					Image:           "foo/bar",
					Heartbeat:       "30m",
					Env:             map[string]string{"KEY": "val"},
					SecretName:      "my-secret",
					VolumeClaim:     "my-pvc",
					VolumeMountPath: "/mnt/data",
				},
			},
		}
		config, err := resolver.Resolve(ctx, "claude")
		Expect(err).To(BeNil())
		Expect(config.Assignee).To(Equal("claude"))
		Expect(config.Image).To(Equal("foo/bar:dev"))
		Expect(config.Env).To(Equal(map[string]string{"KEY": "val"}))
		Expect(config.SecretName).To(Equal("my-secret"))
		Expect(config.VolumeClaim).To(Equal("my-pvc"))
		Expect(config.VolumeMountPath).To(Equal("/mnt/data"))
	})

	It("returns ErrAgentConfigNotFound when no item matches", func() {
		provider.items = []v1.AgentConfig{
			{Spec: v1.AgentConfigSpec{Assignee: "other-agent", Image: "img", Heartbeat: "1m"}},
		}
		_, err := resolver.Resolve(ctx, "claude")
		Expect(err).NotTo(BeNil())
		Expect(errors.Is(err, pkg.ErrAgentConfigNotFound)).To(BeTrue())
	})

	It("returns ErrAgentConfigNotFound when store is empty", func() {
		provider.items = []v1.AgentConfig{}
		_, err := resolver.Resolve(ctx, "claude")
		Expect(err).NotTo(BeNil())
		Expect(errors.Is(err, pkg.ErrAgentConfigNotFound)).To(BeTrue())
	})

	It("returns a wrapped error when provider.Get fails", func() {
		provider.err = errors.Errorf(ctx, "storage unavailable")
		_, err := resolver.Resolve(ctx, "claude")
		Expect(err).NotTo(BeNil())
		Expect(errors.Is(err, pkg.ErrAgentConfigNotFound)).To(BeFalse())
	})

	It(
		"defensively copies env map — mutation after Resolve does not affect returned config",
		func() {
			originalEnv := map[string]string{"KEY": "val"}
			provider.items = []v1.AgentConfig{
				{
					Spec: v1.AgentConfigSpec{
						Assignee:  "claude",
						Image:     "img",
						Heartbeat: "1m",
						Env:       originalEnv,
					},
				},
			}
			config, err := resolver.Resolve(ctx, "claude")
			Expect(err).To(BeNil())
			originalEnv["KEY"] = "mutated"
			Expect(config.Env["KEY"]).To(Equal("val"))
		},
	)

	It("branch tagging: given branch=dev and Image=foo/bar, result has Image==foo/bar:dev", func() {
		provider.items = []v1.AgentConfig{
			{Spec: v1.AgentConfigSpec{Assignee: "claude", Image: "foo/bar", Heartbeat: "1m"}},
		}
		config, err := resolver.Resolve(ctx, "claude")
		Expect(err).To(BeNil())
		Expect(config.Image).To(Equal("foo/bar:dev"))
	})
})
