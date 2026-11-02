// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !unix

package claude

import (
	"context"
	"os/exec"
	"syscall"

	"github.com/bborbe/errors"
)

// credentialSysProcAttr is unavailable on platforms without unix process
// credentials. It exists so the external test package compiles everywhere; the
// only caller that matters is applyCredential, which refuses a non-zero uid.
func credentialSysProcAttr(_ context.Context, _ int) (*syscall.SysProcAttr, error) {
	return nil, nil
}

// applyCredential refuses a non-zero uid on this platform rather than silently
// leaving the child with the parent's uid — a silent no-op would let a
// cross-built binary believe the isolation is active when it is not.
func applyCredential(ctx context.Context, _ *exec.Cmd, uid int) error {
	if uid == 0 {
		return nil
	}
	return errors.Errorf(ctx, "RunAsUID is not supported on this platform")
}
