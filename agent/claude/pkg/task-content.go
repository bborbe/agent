// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

// TaskContent is the raw markdown content of a task file.
type TaskContent string

// String returns the task content as a string.
func (t TaskContent) String() string { return string(t) }
