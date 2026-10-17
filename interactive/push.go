// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

import "github.com/a2aproject/a2a-go/v2/a2asrv/push"

// ServiceOption configures an optional capability of the interactive service. It is the one
// seam through which a capability a service may or may not have is supplied, so a new
// capability never changes an existing caller's call.
type ServiceOption func(*serviceOptions)

// serviceOptions is the optional configuration the caller's ServiceOption values resolved
// to. It stays unexported because the option set is closed: this library ships the option
// constructors, and a caller composes them rather than writing its own.
type serviceOptions struct {
	// push is the caller's push-notification wiring, or nil when the caller supplied none.
	// A nil push is the "not configured" state, and it is the single value both the Agent
	// Card's capability and the handler's wiring are derived from.
	push *pushConfig
}

// pushConfig is the caller's push-notification dependencies, validated as a pair at
// construction.
type pushConfig struct {
	store  push.ConfigStore
	sender push.Sender
}

// newPushConfig returns the validated push dependencies. A nil store or a nil sender is a
// construction failure, not a degraded configuration: the alternative — treating a
// half-supplied option as "not configured" — hands the caller a service whose card
// advertises no push while the caller believes the channel is wired, so the channel is
// inert while looking deployed. A panic at construction is a startup failure the caller
// fixes.
func newPushConfig(store push.ConfigStore, sender push.Sender) *pushConfig {
	if store == nil {
		panic("interactive.WithPushNotifications: push config store is nil")
	}
	if sender == nil {
		panic("interactive.WithPushNotifications: push sender is nil")
	}
	return &pushConfig{store: store, sender: sender}
}

// WithPushNotifications returns the option that wires A2A push notifications through the
// caller's own config store and sender. Supplying the option IS the opt-in: there is no
// second flag that enables push while the option is present, and no flag that disables it
// while the option is present. Both values are passed to the SDK's
// a2asrv.WithPushNotifications untouched — this library constructs neither and overrides
// no field on either, so the caller's sender keeps its own SSRF posture, timeout,
// redirect cap and FailOnError setting.
func WithPushNotifications(store push.ConfigStore, sender push.Sender) ServiceOption {
	return func(options *serviceOptions) {
		options.push = newPushConfig(store, sender)
	}
}

// newServiceOptions resolves the options the caller supplied. No option leaves every field
// at its zero value, which is the "no optional capability is configured" state.
func newServiceOptions(opts ...ServiceOption) *serviceOptions {
	options := &serviceOptions{}
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// pushNotificationsEnabled reports whether the caller wired push notifications. It is the one
// decision the Agent Card's capability and the SDK handler's wiring are both derived from,
// which is what makes "the card advertises support while the handler refuses a registration"
// an unreachable state.
func (o *serviceOptions) pushNotificationsEnabled() bool {
	return o.push != nil
}
