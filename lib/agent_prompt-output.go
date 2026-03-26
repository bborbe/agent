// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

// PromptOutput is the result output produced by an agent job.
type PromptOutput string

func (p PromptOutput) String() string {
	return string(p)
}
