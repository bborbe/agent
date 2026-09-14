// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// humanOriginKind is the provenance marker the Claude CLI writes on user-role
// transcript entries for human-typed input ("origin.kind": "human"). A
// machine-delivered prompt carries no such marker and tool results carry neither,
// so this marker is the whole discriminator between a human intervention and
// machine-driven traffic (spec 053).
const humanOriginKind = "human"

// maxSessionIDLength bounds the session id accepted as a file-name component.
const maxSessionIDLength = 128

// maxTranscriptLineBytes bounds a single transcript line the scanner will read.
const maxTranscriptLineBytes = 10 * 1024 * 1024

// isValidSessionID reports whether sessionID is safe to use as a file-name component
// under the config directory: 1..maxSessionIDLength characters from [A-Za-z0-9-].
// Path separators, traversal sequences, glob metacharacters and whitespace are all
// rejected — the id arrives from a subprocess and is treated as untrusted input.
func isValidSessionID(sessionID string) bool {
	if sessionID == "" || len(sessionID) > maxSessionIDLength {
		return false
	}
	for _, r := range sessionID {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}

// findTranscriptFile returns the path of the session's transcript under
// <configDir>/projects. The CLI writes it as
// <config-dir>/projects/<encoded-cwd>/<session-id>.jsonl; the encoded-cwd segment is
// a CLI implementation detail, so the lookup matches the exact file name
// <session-id>.jsonl in any single directory directly under projects/. The session id
// is validated first and the returned path is always inside <configDir>/projects — a
// rejected id, a missing projects directory, or anything other than exactly one match
// returns ok=false.
func findTranscriptFile(configDir, sessionID string) (string, bool) {
	if !isValidSessionID(sessionID) {
		return "", false
	}
	pattern := filepath.Join(configDir, "projects", "*", sessionID+".jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) != 1 {
		return "", false
	}
	return matches[0], true
}

// transcriptEntry is the subset of a transcript line the scan reads: the entry kind
// and the human-provenance marker. Nothing else is decoded, logged, published, or
// stored.
type transcriptEntry struct {
	Type    string            `json:"type"`
	Origin  *transcriptOrigin `json:"origin"`
	Message *transcriptMsg    `json:"message"`
}

// transcriptOrigin is the provenance object the CLI attaches to an entry.
type transcriptOrigin struct {
	Kind string `json:"kind"`
}

// transcriptMsg is the nested message object, which may carry its own provenance.
type transcriptMsg struct {
	Origin *transcriptOrigin `json:"origin"`
}

// isHumanAuthoredEntry reports whether a transcript line is a user-role entry the CLI
// recorded as human-authored. The marker is read from the entry's own origin object
// and, as a fallback, from the nested message's origin object — the spec pins the
// marker value `human`, not its exact nesting. A line that does not parse returns an
// error: the caller treats an unparseable transcript as unavailable evidence, never
// as an observation.
func isHumanAuthoredEntry(line []byte) (bool, error) {
	var entry transcriptEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return false, err
	}
	if entry.Type != "user" {
		return false, nil
	}
	if entry.Origin != nil && entry.Origin.Kind == humanOriginKind {
		return true, nil
	}
	if entry.Message != nil &&
		entry.Message.Origin != nil &&
		entry.Message.Origin.Kind == humanOriginKind {
		return true, nil
	}
	return false, nil
}

// countHumanAuthoredEntries counts the user-role entries the session's own transcript
// records as human-authored. ok is false whenever the evidence is not decisive — an
// invalid or missing session id, a transcript that cannot be located, opened, read to
// the end, or parsed. Callers must then write nothing at all: an unobserved zero would
// manufacture an unattended-delivery claim (spec 053). A (0, true) result is an
// observation: the transcript was read in full and held no human-authored entry.
func countHumanAuthoredEntries(
	ctx context.Context,
	configDir ClaudeConfigDir,
	sessionID string,
) (int64, bool) {
	path, ok := findTranscriptFile(configDir.String(), sessionID)
	if !ok {
		return 0, false
	}

	file, err := os.Open(
		path,
	) // #nosec G304 -- path confined to the config dir; session id validated
	if err != nil {
		return 0, false
	}
	defer file.Close()

	var count int64
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), maxTranscriptLineBytes)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return 0, false
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		human, err := isHumanAuthoredEntry([]byte(line))
		if err != nil {
			return 0, false
		}
		if human {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, false
	}
	return count, true
}
