// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

// ServerOptionFns exposes the unexported serverOptionFns to the external
// interactive_test package, so a spec can assert the server-wide write deadline
// through github.com/bborbe/http. Test-only: this file is a _test.go file, is
// compiled only into test builds, and adds nothing to the package's public API.
var ServerOptionFns = serverOptionFns
