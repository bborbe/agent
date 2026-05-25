// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bborbe/errors"
)

// PrintResult marshals a Result to JSON and prints to stdout.
func PrintResult(ctx context.Context, result Result) error {
	data, err := json.Marshal(result)
	if err != nil {
		return errors.Wrap(ctx, err, "marshal result")
	}
	fmt.Println(string(data))
	return nil
}

// BuildResultSection renders a Result as a markdown section.
func BuildResultSection(r string) string {
	var sb strings.Builder
	sb.WriteString("## Result\n\n")
	sb.WriteString(r)
	return sb.String()
}
