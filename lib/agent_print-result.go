// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"encoding/json"
	"fmt"
)

// PrintResult marshals a framework Result to JSON and prints to stdout.
// nil result is a no-op (returns nil error). Used by agent main.go entry
// points to surface the terminal step outcome on stderr/stdout for
// log aggregators and the K8s Job exit observer.
func PrintResult(result *Result) error {
	if result == nil {
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	fmt.Println(string(data))
	return nil
}
