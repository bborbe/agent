// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conflict

import (
	"context"
	"fmt"
	"strings"

	"github.com/bborbe/errors"
	"github.com/golang/glog"
	"google.golang.org/genai"
)

// GeminiConflictResolver resolves merge conflicts using the Gemini LLM API.
type GeminiConflictResolver struct {
	apiKey string
}

// NewGeminiConflictResolver creates a new GeminiConflictResolver with the given API key.
func NewGeminiConflictResolver(apiKey string) *GeminiConflictResolver {
	return &GeminiConflictResolver{apiKey: apiKey}
}

// Resolve sends the conflicted file content to Gemini and returns the resolved content.
func (g *GeminiConflictResolver) Resolve(
	ctx context.Context,
	filename string,
	content string,
) (string, error) {
	glog.V(2).Infof("resolving conflict in %s (%d bytes)", filename, len(content))

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  g.apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return "", errors.Wrapf(ctx, err, "create Gemini client")
	}

	prompt := fmt.Sprintf(
		`You are a merge conflict resolver for markdown files. Resolve the conflict markers below and return ONLY the resolved file content with no explanation, no markdown code fences, and no additional commentary.

RESOLUTION RULES:
- Merge both versions intelligently
- For overlapping sections, prefer the content between >>>>>>> markers (the incoming/agent version)
- Remove ALL conflict markers (lines starting with <<<<<<<, =======, >>>>>>>)
- Preserve all non-conflicting content exactly as-is
- Return only the file content, nothing else

FILE: %s

BEGIN FILE CONTENT
%s
END FILE CONTENT`,
		filename,
		content,
	)

	result, err := client.Models.GenerateContent(ctx, "gemini-2.5-flash", genai.Text(prompt), nil)
	if err != nil {
		return "", errors.Wrapf(ctx, err, "Gemini GenerateContent failed")
	}

	resolved := result.Text()

	// Strip markdown code fences that LLMs often wrap output in
	lines := strings.Split(resolved, "\n")
	if len(lines) > 0 {
		firstLine := strings.TrimSpace(lines[0])
		if strings.HasPrefix(firstLine, "```") {
			lines = lines[1:]
		}
	}
	// Remove trailing empty lines before checking for closing fence
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1]
	}
	resolved = strings.Join(lines, "\n")

	// Ensure trailing newline
	if !strings.HasSuffix(resolved, "\n") {
		resolved += "\n"
	}

	glog.V(3).
		Infof("resolved conflict in %s: %d bytes -> %d bytes", filename, len(content), len(resolved))

	return resolved, nil
}
