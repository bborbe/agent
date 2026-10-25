// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"context"
	"encoding/json"
	"math/rand/v2"
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

	// rateLimitMinRemaining is how much time must remain on the context deadline
	// BEYOND the backoff itself for a re-invoke to be worth starting. A retry that
	// cannot finish inside the budget is not started at all: that is the guard
	// against turning a rate-limit crash into a deadline_exceeded crash, which
	// would move the failure rather than remove it.
	//
	// The reserve is the CLI's own retry budget, not a small margin. A re-invoke
	// launched into the same storm will spend up to ~5 minutes exhausting the CLI's
	// ten API retries before it either succeeds or gives up; admitting one with
	// only the backoff plus a few seconds left guarantees it is killed at the
	// deadline mid-run, which is exactly the trade this guard exists to prevent.
	rateLimitMinRemaining = 5 * time.Minute

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
// base, 2×base, 4×base, then the cap, with equal jitter applied on top.
//
// The jitter is not decoration. Several reviewer pods rate-limited by the same
// router would otherwise sleep the same deterministic interval and re-invoke in
// lockstep, converting one 429 burst into the next — the exact amplification the
// exponential growth is meant to avoid.
func (r *claudeRunner) rateLimitBackoff(retries int64) time.Duration {
	base := r.config.RateLimitBackoffBase
	if base <= 0 {
		base = rateLimitBackoffBase
	}
	wait := base << retries
	if wait > rateLimitBackoffMax || wait <= 0 {
		wait = rateLimitBackoffMax
	}
	// Equal jitter: half the interval fixed, half random. Full jitter would admit
	// a near-zero wait and re-create the lockstep it is meant to break.
	half := wait / 2
	if half <= 0 {
		return wait
	}
	return half + rand.N(half) //nolint:gosec // jitter, not a security decision
}

// hasTimeFor reports whether the context deadline leaves room for a backoff of
// `wait` plus the reserve. A context without a deadline is unbounded, so the
// retry is always admitted.
//
// This reads time.Until rather than an injected libtime clock on purpose. The
// rule against time.Now() exists so business logic is testable, and a fake clock
// makes it so; a context deadline is a real-time instant that no injected clock
// can move, so pairing a fake "now" with a real deadline would make the guard
// report a remaining time that does not exist — the one thing it must not do.
// time.Until measures the same real-time interval the context itself uses.
func (r *claudeRunner) hasTimeFor(ctx context.Context, wait time.Duration) bool {
	deadline, ok := ctx.Deadline()
	if !ok {
		return true
	}
	return time.Until(deadline) > wait+rateLimitMinRemaining
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
