// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/bborbe/errors"
	"github.com/golang/glog"
)

// pluginCommanderEnv is the environment handed to `claude plugin …`. The parent's
// full environment is never inherited: this child runs as root, and the parent may
// hold credentials (see ClaudeRunnerConfig.RunAsUID) that the child has no use for.
// CLAUDE_CONFIG_DIR is deliberately omitted, so a session-writable config
// directory is not handed to a root process.
func pluginCommanderEnv() []string {
	var env []string
	for _, k := range []string{
		"PATH", "HOME", "USER", "TZ", "ZONEINFO", "TMPDIR", "LANG", "LC_ALL",
	} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// PluginSpec identifies a Claude Code plugin to ensure is installed.
type PluginSpec struct {
	Marketplace string // e.g. "bborbe/coding"
	Name        string // e.g. "coding"
}

// PluginCommander runs an external command and returns its combined stdout.
//
//counterfeiter:generate -o ../mocks/claude-plugin-commander.go --fake-name ClaudePluginCommander . PluginCommander
type PluginCommander interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// PluginInstaller ensures a list of Claude plugins are installed or updated.
//
//counterfeiter:generate -o ../mocks/claude-plugin-installer.go --fake-name ClaudePluginInstaller . PluginInstaller
type PluginInstaller interface {
	EnsureInstalled(ctx context.Context, specs []PluginSpec) error
}

// NewExecPluginCommander returns a PluginCommander that uses os/exec to run real processes.
func NewExecPluginCommander() PluginCommander {
	return &execPluginCommander{}
}

type execPluginCommander struct{}

func (e *execPluginCommander) Run(
	ctx context.Context,
	name string,
	args ...string,
) (string, error) {
	cmd := exec.CommandContext(
		ctx,
		name,
		args...) // #nosec G204 -- name and args are caller-controlled; PluginCommander is an internal interface not exposed to untrusted input
	// This is a root-run setup path, so it must NOT inherit the parent's
	// environment: the parent may hold credentials (see ClaudeRunnerConfig.RunAsUID)
	// that this child has no use for, and inheriting them would leave them
	// readable from a root child's /proc/<pid>/environ. CLAUDE_CONFIG_DIR is
	// deliberately omitted so a session-writable config directory is never handed
	// to a root process. This path stays root rather than taking RunAsUID: it is
	// setup, it runs before any session exists, and dropping privileges here would
	// make the installed plugin state owned by the session's uid.
	cmd.Env = pluginCommanderEnv()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	runStart := time.Now()
	if err := cmd.Run(); err != nil {
		glog.Infof(
			"%s %s failed after %s: %v",
			name,
			strings.Join(args, " "),
			time.Since(runStart),
			err,
		)
		return "", errors.Wrapf(
			ctx,
			err,
			"run %s %s: %s",
			name,
			strings.Join(args, " "),
			errOut.String(),
		)
	}
	glog.Infof(
		"%s %s returned %d bytes in %s",
		name,
		strings.Join(args, " "),
		out.Len(),
		time.Since(runStart),
	)
	return out.String(), nil
}

// NewPluginInstaller returns a PluginInstaller that uses the given PluginCommander to manage Claude plugins.
func NewPluginInstaller(commander PluginCommander) PluginInstaller {
	return &pluginInstaller{commander: commander}
}

type pluginInstaller struct {
	commander PluginCommander
}

func (i *pluginInstaller) EnsureInstalled(ctx context.Context, specs []PluginSpec) error {
	if len(specs) == 0 {
		return nil
	}
	for _, spec := range specs {
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx, ctx.Err(), "context cancelled during EnsureInstalled")
		default:
		}

		if err := i.ensureOne(ctx, spec); err != nil {
			return errors.Wrap(ctx, err, "ensure plugin installed: "+spec.Name)
		}
	}
	return nil
}

func (i *pluginInstaller) ensureOne(ctx context.Context, spec PluginSpec) error {
	alias := path.Base(spec.Marketplace)
	updateForm := spec.Name + "@" + alias

	glog.Infof("spec=%s action=%s", spec.Name, "claude plugin list")
	output, err := i.commander.Run(ctx, "claude", "plugin", "list")
	if err != nil {
		return errors.Wrapf(ctx, err, "list plugins")
	}

	installed := false
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, spec.Name) {
			installed = true
			break
		}
	}

	if !installed {
		glog.Infof("spec=%s action=%s", spec.Name, "marketplace add")
		if err := i.runHard(ctx, "claude", "plugin", "marketplace", "add", spec.Marketplace); err != nil {
			return errors.Wrap(ctx, err, "run marketplace add: "+spec.Marketplace)
		}
		glog.Infof("spec=%s action=%s", spec.Name, "plugin install")
		if err := i.runHard(ctx, "claude", "plugin", "install", spec.Name); err != nil {
			return errors.Wrap(ctx, err, "run plugin install: "+spec.Name)
		}
		return nil
	}

	glog.Infof("spec=%s action=%s", spec.Name, "marketplace update")
	if _, err := i.commander.Run(ctx, "claude", "plugin", "marketplace", "update", alias); err != nil {
		glog.Warningf(
			"marketplace update failed plugin=%s cmd=%s err=%v",
			spec.Name,
			"claude plugin marketplace update "+alias,
			err,
		)
	}
	glog.Infof("spec=%s action=%s", spec.Name, "plugin update")
	if _, err := i.commander.Run(ctx, "claude", "plugin", "update", updateForm); err != nil {
		glog.Warningf(
			"plugin update failed plugin=%s cmd=%s err=%v",
			spec.Name,
			"claude plugin update "+updateForm,
			err,
		)
	}
	return nil
}

func (i *pluginInstaller) runHard(ctx context.Context, name string, args ...string) error {
	_, err := i.commander.Run(ctx, name, args...)
	if err != nil {
		return errors.Wrapf(ctx, err, "run %s %s", name, strings.Join(args, " "))
	}
	return nil
}
