// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

const (
	// maxRateLimitReinvokes caps how many times one Run re-invokes the CLI after it
	// exhausted its own API retries on a rate limit. Three re-invokes with the
	// backoff below cover roughly three and a half minutes of a retry storm, which
	// fits inside the 25 m soft phase budget a reviewer gives one Claude call
	// without eating the salvage headroom.
	maxRateLimitReinvokes = 3

	// rateLimitBackoffBase is the first backoff interval. Each re-invoke doubles it.
	rateLimitBackoffBase = 15 * time.Second

	// rateLimitBackoffMax caps a single backoff interval, so a raised
	// maxRateLimitReinvokes cannot stretch one sleep past a useful length.
	rateLimitBackoffMax = 2 * time.Minute

	// rateLimitDeadlineFloor is how much time must remain on the context deadline
	// BEYOND the backoff itself for a re-invoke to be worth starting. A retry that
	// cannot finish inside the budget is not started at all: that is the guard
	// against turning a rate-limit crash into a deadline_exceeded crash, which
	// would move the failure rather than remove it.
	rateLimitDeadlineFloor = 30 * time.Second

	// httpStatusTooManyRequests and httpStatusOverloaded are the two capacity
	// responses the CLI retries and eventually gives up on.
	httpStatusTooManyRequests = 429
	httpStatusOverloaded      = 529
)

// rateLimitStatus reports the HTTP status of a rate limit the CLI gave up on, or
// 0 when the tail names none.
//
// The evidence is the CLI's own retry stream, not the exit code: a non-zero exit
// says "failed", and only the `api_retry` events say whether the cause was
// capacity. The LAST retry event is the one that counts — an earlier attempt is
// superseded by a later one — and it is read as terminal only once the CLI has
// spent its whole retry budget, since a mid-budget event means the CLI was still
// going to retry itself.
//
// A `result` event marked `is_error` whose text names a rate limit is the second
// shape: some CLI versions summarise the storm there instead of leaving a final
// api_retry line.
func rateLimitStatus(tail []string) int {
	var lastRetry *failureEvent
	var resultErr string

	for _, line := range tail {
		var event failureEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.Type == "system" && event.Subtype == "api_retry" {
			retry := event
			lastRetry = &retry
		}
		if event.Type == "result" && event.IsError && event.Result != "" {
			resultErr = event.Result
		}
	}

	if lastRetry != nil && isCapacityStatus(lastRetry.ErrorStatus) {
		if lastRetry.MaxRetries == 0 || lastRetry.Attempt >= lastRetry.MaxRetries {
			return lastRetry.ErrorStatus
		}
	}

	if namesRateLimit(resultErr) {
		return httpStatusTooManyRequests
	}
	return 0
}

// isCapacityStatus reports whether an HTTP status is one of the two capacity
// responses worth re-invoking for.
func isCapacityStatus(status int) bool {
	return status == httpStatusTooManyRequests || status == httpStatusOverloaded
}

// namesRateLimit reports whether free text names a rate limit or an overload.
// Used only for the `result.is_error` shape, where no status code is carried.
func namesRateLimit(text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	return strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "rate_limit") ||
		strings.Contains(lower, "overloaded")
}

// rateLimitBackoff returns the wait before re-invoke number `retries` (0-based):
// base, 2×base, 4×base, then the cap. Exponential so a storm is not amplified by
// a lockstep client, capped so a single wait stays useful.
func (r *claudeRunner) rateLimitBackoff(retries int64) time.Duration {
	base := r.config.RateLimitBackoffBase
	if base <= 0 {
		base = rateLimitBackoffBase
	}
	wait := base << retries
	if wait > rateLimitBackoffMax || wait <= 0 {
		return rateLimitBackoffMax
	}
	return wait
}

// hasTimeFor reports whether the context deadline leaves room for a backoff of
// `wait` plus the floor. A context without a deadline is unbounded, so the retry
// is always admitted.
func (r *claudeRunner) hasTimeFor(ctx context.Context, wait time.Duration) bool {
	deadline, ok := ctx.Deadline()
	if !ok {
		return true
	}
	return deadline.Sub(r.currentDateTime.Now().Time()) > wait+rateLimitDeadlineFloor
}

// sleepCtx waits for d or until the context is done, whichever comes first. A
// cancelled context returns its error so the caller stops instead of retrying
// into a dead budget.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
