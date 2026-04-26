// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/bborbe/errors"
)

// Schema helpers — typed JSON inside markdown body sections.
//
// Body sections are the inter-step communication format: AI writes them
// naturally as markdown, code reads them deterministically as typed JSON.
//
// Two operations:
//   - ExtractSection[T]:    read a section, parse its fenced JSON into T
//   - MarshalSectionTyped:  render a typed value as a Section ready for
//                           markdown.AddSection / ReplaceSection

var jsonFenceRE = regexp.MustCompile("(?s)```json\\s*\\n(.*?)\\n```")

// ExtractSection reads `heading` from the markdown, finds the first
// ```json fence inside its body, unmarshals into T, and returns a typed
// pointer.
//
// Errors are formatted for use as needs_input messages:
//   - "<heading> section missing" — heading not found
//   - "<heading>: json block missing" — no ```json fence in section
//   - "<heading>: json malformed: <detail>" — unmarshal failed
func ExtractSection[T any](ctx context.Context, md *Markdown, heading string) (*T, error) {
	section, ok := md.FindSection(heading)
	if !ok {
		return nil, errors.Errorf(ctx, "%s section missing", heading)
	}
	matches := jsonFenceRE.FindStringSubmatch(section.Body)
	if len(matches) < 2 {
		return nil, errors.Errorf(ctx, "%s: json block missing", heading)
	}
	var out T
	if err := json.Unmarshal([]byte(matches[1]), &out); err != nil {
		return nil, errors.Wrapf(ctx, err, "%s: json malformed", heading)
	}
	return &out, nil
}

// ExtractSectionMap is the untyped variant — useful when the schema is
// not known statically.
func ExtractSectionMap(ctx context.Context, md *Markdown, heading string) (map[string]any, error) {
	out, err := ExtractSection[map[string]any](ctx, md, heading)
	if err != nil {
		return nil, err
	}
	return *out, nil
}

// MarshalSectionTyped renders a typed value as a Section ready for
// markdown.AddSection or markdown.ReplaceSection.
//
// Output Section.Body format:
//
//	```json
//	{
//	  "field": "value"
//	}
//	```
//
// Round-trips with ExtractSection for the same heading and type.
func MarshalSectionTyped[T any](ctx context.Context, heading string, value T) (Section, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return Section{}, errors.Wrapf(ctx, err, "marshal %s", heading)
	}
	body := fmt.Sprintf("```json\n%s\n```", string(data))
	return Section{Heading: heading, Body: body}, nil
}
