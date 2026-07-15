// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pi_test

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/pi"
)

var _ = Describe("piRunner cwd", func() {
	var (
		ctx          context.Context
		shimDir      string
		originalPath string
	)

	BeforeEach(func() {
		ctx = context.Background()
		shimDir = GinkgoT().TempDir()
		shimPath := filepath.Join(shimDir, "pi")
		// Shim emits JSON in the format pi --mode json produces.
		// "$PWD" lets us assert the working directory pi was spawned in.
		script := `#!/bin/sh
printf '{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"CWD=%s"}]}]}\n' "$PWD"
`
		Expect(os.WriteFile(shimPath, []byte(script), 0755)).To(Succeed()) //nolint:gosec
		originalPath = os.Getenv("PATH")
		Expect(os.Setenv("PATH", shimDir+":"+originalPath)).To(Succeed())
		DeferCleanup(func() {
			Expect(os.Setenv("PATH", originalPath)).To(Succeed())
		})
	})

	It("spawns pi with cwd = AgentDir when AgentDir is set", func() {
		workDir, err := filepath.EvalSymlinks(GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		runner := pi.NewRunner(pi.PiRunnerConfig{AgentDir: workDir})
		result, err := runner.Run(ctx, "test")
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Result).To(ContainSubstring("CWD=" + workDir))
	})

	It("inherits parent cwd when AgentDir is empty", func() {
		parentCwd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		parentCwd, err = filepath.EvalSymlinks(parentCwd)
		Expect(err).NotTo(HaveOccurred())
		runner := pi.NewRunner(pi.PiRunnerConfig{})
		result, err := runner.Run(ctx, "test")
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Result).To(ContainSubstring("CWD=" + parentCwd))
	})
})
