// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

// TaskContent is the markdown body of a task.
type TaskContent string

func (t TaskContent) String() string {
	return string(t)
}
