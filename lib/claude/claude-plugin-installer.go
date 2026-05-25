// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"bytes"
	"context"
	"os/exec"
	"path"
	"strings"

	"github.com/bborbe/errors"
	"github.com/golang/glog"
)

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
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return "", errors.Wrapf(
			ctx,
			err,
			"run %s %s: %s",
			name,
			strings.Join(args, " "),
			errOut.String(),
		)
	}
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
		if err := i.runHard(ctx, "claude", "plugin", "marketplace", "add", spec.Marketplace); err != nil {
			return errors.Wrap(ctx, err, "run marketplace add: "+spec.Marketplace)
		}
		if err := i.runHard(ctx, "claude", "plugin", "install", spec.Name); err != nil {
			return errors.Wrap(ctx, err, "run plugin install: "+spec.Name)
		}
		return nil
	}

	if _, err := i.commander.Run(ctx, "claude", "plugin", "marketplace", "update", alias); err != nil {
		glog.Warningf(
			"marketplace update failed plugin=%s cmd=%s err=%v",
			spec.Name,
			"claude plugin marketplace update "+alias,
			err,
		)
	}
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
