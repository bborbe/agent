// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/bborbe/errors"
)

// expandTilde returns the argument with a leading "~/" replaced by the
// current user's home directory (via os.UserHomeDir). Empty input returns
// empty. Inputs that do not begin with "~/" are returned unchanged.
//
// The lone "~" (without trailing slash) is also expanded — same as shell
// semantics. Any other "~"-prefixed form (e.g. "~user/...") is NOT expanded
// and is returned unchanged; the caller is expected to use the literal.
func expandTilde(ctx context.Context, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrapf(ctx, err, "resolve user home directory for path %q", path)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}
