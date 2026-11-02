// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

// CredentialSysProcAttr exposes the unexported credentialSysProcAttr to the
// external claude_test package, so a spec can assert its range check directly.
// Test-only: this file is a _test.go file, is compiled only into test builds,
// and adds nothing to the package's public API.
var CredentialSysProcAttr = credentialSysProcAttr
