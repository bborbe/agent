// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

// PromptMessage is a human-readable message from an agent job result.
type PromptMessage string

func (p PromptMessage) String() string {
	return string(p)
}
