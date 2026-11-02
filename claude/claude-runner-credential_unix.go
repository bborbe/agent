// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build unix

package claude

import (
	"context"
	"math"
	"syscall"

	"github.com/bborbe/errors"
)

// credentialSysProcAttr returns the SysProcAttr that runs a child as uid.
func credentialSysProcAttr(ctx context.Context, uid int) (*syscall.SysProcAttr, error) {
	if uid < 1 || uint64(uid) > math.MaxUint32 {
		return nil, errors.Errorf(ctx, "uid %d out of range", uid)
	}
	return &syscall.SysProcAttr{Credential: &syscall.Credential{
		Uid:    uint32(uid),
		Gid:    uint32(uid),
		Groups: []uint32{},
	}}, nil
}
