// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude_test

import (
	"context"
	stderrors "errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/claude"
	"github.com/bborbe/agent/mocks"
)

var _ = Describe("ExecPluginCommander", func() {
	var ctx context.Context
	var commander claude.PluginCommander

	BeforeEach(func() {
		ctx = context.Background()
		commander = claude.NewExecPluginCommander()
	})

	Context("Run", func() {
		Context("with a command that succeeds", func() {
			var output string
			var err error

			JustBeforeEach(func() {
				output, err = commander.Run(ctx, "echo", "hello")
			})

			It("returns nil error", func() {
				Expect(err).To(BeNil())
			})

			It("returns stdout", func() {
				Expect(output).To(ContainSubstring("hello"))
			})
		})

		Context("with a command that fails", func() {
			var err error

			JustBeforeEach(func() {
				_, err = commander.Run(ctx, "false")
			})

			It("returns non-nil error", func() {
				Expect(err).NotTo(BeNil())
			})
		})
	})
})

var _ = Describe("PluginInstaller", func() {
	var ctx context.Context
	var fakeCommander *mocks.ClaudePluginCommander
	var installer claude.PluginInstaller
	var err error

	BeforeEach(func() {
		ctx = context.Background()
		fakeCommander = &mocks.ClaudePluginCommander{}
		installer = claude.NewPluginInstaller(fakeCommander)
	})

	Context("EnsureInstalled", func() {
		Context("empty input", func() {
			JustBeforeEach(func() {
				err = installer.EnsureInstalled(ctx, nil)
			})

			It("returns nil", func() {
				Expect(err).To(BeNil())
			})

			It("does not invoke the commander", func() {
				Expect(fakeCommander.RunCallCount()).To(Equal(0))
			})
		})

		Context("install path (plugin not in list)", func() {
			BeforeEach(func() {
				fakeCommander.RunReturnsOnCall(0, "other-plugin\n", nil)
				fakeCommander.RunReturns("", nil)
			})

			JustBeforeEach(func() {
				err = installer.EnsureInstalled(ctx, []claude.PluginSpec{
					{Marketplace: "bborbe/coding", Name: "coding"},
				})
			})

			It("returns nil", func() {
				Expect(err).To(BeNil())
			})

			It("calls Run 3 times", func() {
				Expect(fakeCommander.RunCallCount()).To(Equal(3))
			})

			It("calls plugin list first", func() {
				_, name, args := fakeCommander.RunArgsForCall(0)
				Expect(name).To(Equal("claude"))
				Expect(args).To(Equal([]string{"plugin", "list"}))
			})

			It("calls marketplace add second", func() {
				_, name, args := fakeCommander.RunArgsForCall(1)
				Expect(name).To(Equal("claude"))
				Expect(args).To(Equal([]string{"plugin", "marketplace", "add", "bborbe/coding"}))
			})

			It("calls plugin install third", func() {
				_, name, args := fakeCommander.RunArgsForCall(2)
				Expect(name).To(Equal("claude"))
				Expect(args).To(Equal([]string{"plugin", "install", "coding"}))
			})
		})

		Context("update path (plugin is in list)", func() {
			BeforeEach(func() {
				fakeCommander.RunReturnsOnCall(0, "coding v1.0\n", nil)
				fakeCommander.RunReturns("", nil)
			})

			JustBeforeEach(func() {
				err = installer.EnsureInstalled(ctx, []claude.PluginSpec{
					{Marketplace: "bborbe/coding", Name: "coding"},
				})
			})

			It("returns nil", func() {
				Expect(err).To(BeNil())
			})

			It("calls Run 3 times", func() {
				Expect(fakeCommander.RunCallCount()).To(Equal(3))
			})

			It("calls marketplace update second", func() {
				_, name, args := fakeCommander.RunArgsForCall(1)
				Expect(name).To(Equal("claude"))
				Expect(args).To(Equal([]string{"plugin", "marketplace", "update", "coding"}))
			})

			It("calls plugin update third", func() {
				_, name, args := fakeCommander.RunArgsForCall(2)
				Expect(name).To(Equal("claude"))
				Expect(args).To(Equal([]string{"plugin", "update", "coding@coding"}))
			})
		})

		Context("list failure", func() {
			BeforeEach(func() {
				fakeCommander.RunReturns("", stderrors.New("exec failed"))
			})

			JustBeforeEach(func() {
				err = installer.EnsureInstalled(ctx, []claude.PluginSpec{
					{Marketplace: "bborbe/coding", Name: "coding"},
				})
			})

			It("returns non-nil error", func() {
				Expect(err).NotTo(BeNil())
			})

			It("error contains list plugins", func() {
				Expect(err.Error()).To(ContainSubstring("list plugins"))
			})
		})

		Context("marketplace add failure (install path)", func() {
			BeforeEach(func() {
				fakeCommander.RunReturnsOnCall(0, "", nil)
				fakeCommander.RunReturnsOnCall(1, "", stderrors.New("marketplace add failed"))
			})

			JustBeforeEach(func() {
				err = installer.EnsureInstalled(ctx, []claude.PluginSpec{
					{Marketplace: "bborbe/coding", Name: "coding"},
				})
			})

			It("returns non-nil error", func() {
				Expect(err).NotTo(BeNil())
			})
		})

		Context("plugin install failure (install path)", func() {
			BeforeEach(func() {
				fakeCommander.RunReturnsOnCall(0, "", nil)
				fakeCommander.RunReturnsOnCall(1, "", nil)
				fakeCommander.RunReturnsOnCall(2, "", stderrors.New("install failed"))
			})

			JustBeforeEach(func() {
				err = installer.EnsureInstalled(ctx, []claude.PluginSpec{
					{Marketplace: "bborbe/coding", Name: "coding"},
				})
			})

			It("returns non-nil error", func() {
				Expect(err).NotTo(BeNil())
			})
		})

		Context("soft failure: marketplace update (update path)", func() {
			BeforeEach(func() {
				fakeCommander.RunReturnsOnCall(0, "coding v1.0\n", nil)
				fakeCommander.RunReturnsOnCall(1, "", stderrors.New("marketplace update failed"))
				fakeCommander.RunReturnsOnCall(2, "", nil)
			})

			JustBeforeEach(func() {
				err = installer.EnsureInstalled(ctx, []claude.PluginSpec{
					{Marketplace: "bborbe/coding", Name: "coding"},
				})
			})

			It("returns nil (soft failure)", func() {
				Expect(err).To(BeNil())
			})

			It("still attempts plugin update", func() {
				Expect(fakeCommander.RunCallCount()).To(Equal(3))
				_, name, args := fakeCommander.RunArgsForCall(2)
				Expect(name).To(Equal("claude"))
				Expect(args).To(Equal([]string{"plugin", "update", "coding@coding"}))
			})
		})

		Context("soft failure: plugin update (update path)", func() {
			BeforeEach(func() {
				fakeCommander.RunReturnsOnCall(0, "coding v1.0\n", nil)
				fakeCommander.RunReturnsOnCall(1, "", nil)
				fakeCommander.RunReturnsOnCall(2, "", stderrors.New("plugin update failed"))
			})

			JustBeforeEach(func() {
				err = installer.EnsureInstalled(ctx, []claude.PluginSpec{
					{Marketplace: "bborbe/coding", Name: "coding"},
				})
			})

			It("returns nil (soft failure)", func() {
				Expect(err).To(BeNil())
			})
		})

		Context("context cancellation", func() {
			BeforeEach(func() {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
				fakeCommander.RunStub = func(c context.Context, name string, args ...string) (string, error) {
					return "", c.Err()
				}
			})

			JustBeforeEach(func() {
				err = installer.EnsureInstalled(ctx, []claude.PluginSpec{
					{Marketplace: "bborbe/coding", Name: "coding"},
				})
			})

			It("returns non-nil error", func() {
				Expect(err).NotTo(BeNil())
			})
		})
	})
})
