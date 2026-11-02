// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !unix

package claude

import (
	"context"
	"syscall"
)

// credentialSysProcAttr is a no-op on platforms without unix process credentials.
func credentialSysProcAttr(_ context.Context, _ int) (*syscall.SysProcAttr, error) {
	return nil, nil
}
