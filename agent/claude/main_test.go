// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

var _ = Describe("Main", func() {
	It("Compiles", func() {
		tmpDir, err := os.MkdirTemp("", "agent-claude-build-*")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)
		cmd := exec.Command("go", "build", "-mod=mod", "-buildvcs=false", "-o", filepath.Join(tmpDir, "bin"), ".")
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "go build failed: %s", out)
	})
})

func TestSuite(t *testing.T) {
	time.Local = time.UTC
	format.TruncatedDiff = false
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.Timeout = 60 * time.Second
	RunSpecs(t, "Main Suite", suiteConfig, reporterConfig)
}
