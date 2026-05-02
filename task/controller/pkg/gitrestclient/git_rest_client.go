// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gitrestclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bborbe/errors"

	"github.com/bborbe/agent/task/controller/pkg/metrics"
)

//counterfeiter:generate -o ../../mocks/git_rest_client.go --fake-name FakeGitRestClient . GitRestClient

// GitRestClient is the HTTP client for git-rest's /api/v1/files REST API.
// All paths are relative to the repo root (e.g. "tasks/foo.md").
type GitRestClient interface {
	// Get retrieves the current content of the file at relPath.
	Get(ctx context.Context, relPath string) ([]byte, error)
	// Post writes content to relPath; git-rest auto-commits and pushes.
	Post(ctx context.Context, relPath string, content []byte) error
	// Delete removes the file at relPath; git-rest auto-commits and pushes.
	Delete(ctx context.Context, relPath string) error
	// List returns relative paths matching the single-level glob pattern (e.g. "tasks/*.md").
	List(ctx context.Context, glob string) ([]string, error)
	// IsReady reports whether git-rest's /readiness returns 200.
	// Returns (false, nil) when git-rest returns 503 — that is a valid not-ready state, not an error.
	// Returns (false, err) only on network failure or unexpected response.
	IsReady(ctx context.Context) (bool, error)
}

// NewGitRestClient creates a GitRestClient targeting the git-rest instance at baseURL.
// baseURL example: "http://vault-obsidian-openclaw:9090"
func NewGitRestClient(baseURL string) GitRestClient {
	return newGitRestClientWithBackoff(baseURL, exponentialBackoff)
}

func newGitRestClientWithBackoff(
	baseURL string,
	backoff func(attempt int) time.Duration,
) GitRestClient {
	return &gitRestClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
		backoff:    backoff,
	}
}

func exponentialBackoff(attempt int) time.Duration {
	return time.Duration(
		1<<uint(attempt-1),
	) * time.Second // #nosec G115 -- attempt is always >= 1 when called
}

type gitRestClient struct {
	baseURL    string
	httpClient *http.Client
	backoff    func(attempt int) time.Duration
}

// Get retrieves file content from git-rest. Does not retry — reads fail-fast.
func (g *gitRestClient) Get(ctx context.Context, relPath string) ([]byte, error) {
	reqURL := g.baseURL + "/api/v1/files/" + relPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		metrics.GitRestCallsTotal.WithLabelValues("get", "error").Inc()
		return nil, errors.Wrapf(ctx, err, "create GET request for %s", relPath)
	}
	resp, err := g.httpClient.Do(req)
	if err != nil {
		metrics.GitRestCallsTotal.WithLabelValues("get", "error").Inc()
		return nil, errors.Wrapf(ctx, err, "GET %s", relPath)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		metrics.GitRestCallsTotal.WithLabelValues("get", "error").Inc()
		return nil, errors.Wrapf(ctx, err, "read GET response body for %s", relPath)
	}
	if resp.StatusCode != http.StatusOK {
		metrics.GitRestCallsTotal.WithLabelValues("get", "error").Inc()
		preview := string(body)
		if len(preview) > 100 {
			preview = preview[:100]
		}
		return nil, errors.Errorf(ctx, "GET %s returned %d: %s", relPath, resp.StatusCode, preview)
	}
	metrics.GitRestCallsTotal.WithLabelValues("get", "success").Inc()
	return body, nil
}

// Post writes content to relPath with retry on 5xx or network errors.
func (g *gitRestClient) Post(ctx context.Context, relPath string, content []byte) error {
	reqURL := g.baseURL + "/api/v1/files/" + relPath
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			metrics.KafkaConsumePausedTotal.Inc()
			backoff := g.backoff(attempt)
			select {
			case <-ctx.Done():
				return errors.Wrapf(ctx, ctx.Err(), "POST %s cancelled during backoff", relPath)
			case <-time.After(backoff):
			}
		}
		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			reqURL,
			bytes.NewReader(content),
		)
		if err != nil {
			metrics.GitRestCallsTotal.WithLabelValues("post", "error").Inc()
			lastErr = errors.Wrapf(ctx, err, "create POST request for %s", relPath)
			continue
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := g.httpClient.Do(req)
		if err != nil {
			metrics.GitRestCallsTotal.WithLabelValues("post", "error").Inc()
			lastErr = errors.Wrapf(ctx, err, "POST %s attempt %d", relPath, attempt+1)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			metrics.GitRestCallsTotal.WithLabelValues("post", "success").Inc()
			return nil
		}
		metrics.GitRestCallsTotal.WithLabelValues("post", "error").Inc()
		lastErr = errors.Errorf(
			ctx,
			"POST %s attempt %d returned %d",
			relPath,
			attempt+1,
			resp.StatusCode,
		)
	}
	return errors.Wrapf(ctx, lastErr, "POST %s failed after 5 attempts", relPath)
}

// Delete removes the file at relPath with retry on 5xx or network errors.
func (g *gitRestClient) Delete(ctx context.Context, relPath string) error {
	reqURL := g.baseURL + "/api/v1/files/" + relPath
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			metrics.KafkaConsumePausedTotal.Inc()
			backoff := g.backoff(attempt)
			select {
			case <-ctx.Done():
				return errors.Wrapf(ctx, ctx.Err(), "DELETE %s cancelled during backoff", relPath)
			case <-time.After(backoff):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
		if err != nil {
			metrics.GitRestCallsTotal.WithLabelValues("delete", "error").Inc()
			lastErr = errors.Wrapf(ctx, err, "create DELETE request for %s", relPath)
			continue
		}
		resp, err := g.httpClient.Do(req)
		if err != nil {
			metrics.GitRestCallsTotal.WithLabelValues("delete", "error").Inc()
			lastErr = errors.Wrapf(ctx, err, "DELETE %s attempt %d", relPath, attempt+1)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			metrics.GitRestCallsTotal.WithLabelValues("delete", "success").Inc()
			return nil
		}
		metrics.GitRestCallsTotal.WithLabelValues("delete", "error").Inc()
		lastErr = errors.Errorf(
			ctx,
			"DELETE %s attempt %d returned %d",
			relPath,
			attempt+1,
			resp.StatusCode,
		)
	}
	return errors.Wrapf(ctx, lastErr, "DELETE %s failed after 5 attempts", relPath)
}

// List returns paths matching the glob pattern. Does not retry — reads fail-fast.
func (g *gitRestClient) List(ctx context.Context, glob string) ([]string, error) {
	reqURL := g.baseURL + "/api/v1/files/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		metrics.GitRestCallsTotal.WithLabelValues("list", "error").Inc()
		return nil, errors.Wrapf(ctx, err, "create LIST request for glob %s", glob)
	}
	q := url.Values{}
	q.Set("glob", glob)
	req.URL.RawQuery = q.Encode()
	resp, err := g.httpClient.Do(req)
	if err != nil {
		metrics.GitRestCallsTotal.WithLabelValues("list", "error").Inc()
		return nil, errors.Wrapf(ctx, err, "LIST glob %s", glob)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		metrics.GitRestCallsTotal.WithLabelValues("list", "error").Inc()
		return nil, errors.Wrapf(ctx, err, "read LIST response body for glob %s", glob)
	}
	if resp.StatusCode != http.StatusOK {
		metrics.GitRestCallsTotal.WithLabelValues("list", "error").Inc()
		preview := string(body)
		if len(preview) > 100 {
			preview = preview[:100]
		}
		return nil, errors.Errorf(
			ctx,
			"LIST glob %s returned %d: %s",
			glob,
			resp.StatusCode,
			preview,
		)
	}
	var paths []string
	if err := json.Unmarshal(body, &paths); err != nil {
		metrics.GitRestCallsTotal.WithLabelValues("list", "error").Inc()
		return nil, errors.Wrapf(ctx, err, "parse LIST response for glob %s", glob)
	}
	metrics.GitRestCallsTotal.WithLabelValues("list", "success").Inc()
	return paths, nil
}

//counterfeiter:generate -o ../../mocks/git_client.go --fake-name FakeGitClient . GitClient

// GitClient is the interface for vault file operations used throughout the controller.
type GitClient interface {
	// EnsureCloned clones the repo if not present, validates if already cloned.
	EnsureCloned(ctx context.Context) error
	// Pull runs git pull on the local clone.
	Pull(ctx context.Context) error
	// CommitAndPush stages all changes, creates a commit with the given message, and pushes to the remote.
	CommitAndPush(ctx context.Context, message string) error
	// AtomicWriteAndCommitPush writes content to absPath and commits+pushes under a single lock.
	// Atomicity (no interleaving with other writes) is guaranteed by the implementation:
	// - gitClient (local-disk): sync.Mutex around the whole sequence.
	// - gitRestGitClientAdapter: relies on per-task serialization (Kafka partitioning by task_id).
	AtomicWriteAndCommitPush(
		ctx context.Context,
		absPath string,
		content []byte,
		message string,
	) error
	// AtomicReadModifyWriteAndCommitPush reads absPath, calls modify on its contents
	// to produce new contents, writes the result, and commits+pushes.
	// Atomicity (no interleaving with other writes) is guaranteed by the implementation:
	// - gitClient (local-disk): sync.Mutex around the whole sequence.
	// - gitRestGitClientAdapter: relies on per-task serialization (Kafka partitioning by task_id).
	// modify must return the new file bytes or an error.
	// If modify returns an error, the file is not written and no commit is made.
	AtomicReadModifyWriteAndCommitPush(
		ctx context.Context,
		absPath string,
		modify func(current []byte) ([]byte, error),
		message string,
	) error
	// Path returns the local clone path.
	Path() string
	// ListFiles returns relative file paths under the repo root matching the single-level
	// glob pattern (e.g. "tasks/*.md"). Paths are relative to the repo root.
	ListFiles(ctx context.Context, glob string) ([]string, error)
	// ReadFile reads the file at relPath (relative to repo root, e.g. "tasks/foo.md")
	// and returns its content.
	ReadFile(ctx context.Context, relPath string) ([]byte, error)
	// WriteFile writes content to relPath (relative to repo root) on local disk.
	// It does NOT commit or push — use AtomicWriteAndCommitPush for that.
	WriteFile(ctx context.Context, relPath string, content []byte) error
}

// IsReady checks git-rest's /readiness endpoint.
func (g *gitRestClient) IsReady(ctx context.Context) (bool, error) {
	reqURL := g.baseURL + "/readiness"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		metrics.GitRestCallsTotal.WithLabelValues("readiness", "error").Inc()
		return false, errors.Wrapf(ctx, err, "create readiness request")
	}
	resp, err := g.httpClient.Do(req)
	if err != nil {
		metrics.GitRestCallsTotal.WithLabelValues("readiness", "error").Inc()
		return false, errors.Wrapf(ctx, err, "readiness check")
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		metrics.GitRestCallsTotal.WithLabelValues("readiness", "success").Inc()
		return true, nil
	case http.StatusServiceUnavailable:
		metrics.GitRestCallsTotal.WithLabelValues("readiness", "success").Inc()
		return false, nil
	default:
		metrics.GitRestCallsTotal.WithLabelValues("readiness", "error").Inc()
		return false, errors.Errorf(ctx, "readiness returned unexpected status %d", resp.StatusCode)
	}
}
