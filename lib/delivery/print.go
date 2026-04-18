// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bborbe/errors"
)

// PrintResult marshals any value to JSON and prints to stdout.
func PrintResult(ctx context.Context, result interface{}) error {
	data, err := json.Marshal(result)
	if err != nil {
		return errors.Wrapf(ctx, err, "marshal result")
	}
	fmt.Println(string(data))
	return nil
}
