// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"fmt"
	"strings"
)

// Instructions is a list of named prompt sections.
type Instructions []Instruction

// Strings returns each instruction rendered as an XML-tagged string.
func (ii Instructions) Strings() []string {
	result := make([]string, len(ii))
	for i, inst := range ii {
		result[i] = inst.String()
	}
	return result
}

// String renders all instructions separated by newlines.
func (ii Instructions) String() string {
	return strings.Join(ii.Strings(), "\n")
}

// Instruction is a named prompt section wrapped in XML tags.
type Instruction struct {
	Name    string
	Content string
}

// String renders the instruction as an XML-tagged block.
func (i Instruction) String() string {
	return fmt.Sprintf("<%s>%s</%s>", i.Name, i.Content, i.Name)
}
