// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build unix

package claude

import (
	"context"
	"math"
	"os/exec"
	"syscall"

	"github.com/bborbe/errors"
	"github.com/golang/glog"
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

// applyCredential sets the privilege drop on cmd when uid is non-zero. It is the
// single implementation both spawn sites call, so their semantics cannot drift.
//
// The guard is `!= 0`, not `> 0`: a negative uid is a misconfiguration and must
// fail loudly through credentialSysProcAttr's range check rather than silently
// leaving the child with the parent's uid — a fail-open on a security boundary.
//
// The Credential field is assigned onto an existing SysProcAttr rather than
// replacing the struct, so a future Setpgid / Pdeathsig on either spawn site is
// not silently dropped.
func applyCredential(ctx context.Context, cmd *exec.Cmd, uid int) error {
	if uid == 0 {
		return nil
	}
	attr, err := credentialSysProcAttr(ctx, uid)
	if err != nil {
		return errors.Wrap(ctx, err, "build credential")
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Credential = attr.Credential
	glog.V(2).Infof("claude CLI will run as uid %d", uid)
	return nil
}
