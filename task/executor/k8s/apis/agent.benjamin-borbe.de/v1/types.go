// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package v1

import (
	"context"
	"reflect"

	"github.com/bborbe/errors"
	libk8s "github.com/bborbe/k8s"
	"github.com/bborbe/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// var _ k8s.Type = Config{} ensures Config implements k8s.Type at compile time.
var _ libk8s.Type = Config{}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Config declares a single agent type that the executor can spawn.
type Config struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec holds the configuration for this agent type.
	Spec ConfigSpec `json:"spec"`
}

// ConfigSpec defines the desired state of a Config.
type ConfigSpec struct {
	// Assignee is the task frontmatter assignee value that routes to this agent.
	Assignee string `json:"assignee"`
	// Image is the container image base name (without tag).
	Image string `json:"image"`
	// Heartbeat is the interval at which the agent re-spawns (e.g. "30m").
	Heartbeat string `json:"heartbeat"`
	// Resources holds optional resource requests for the agent container.
	Resources *AgentResources `json:"resources,omitempty"`
	// Env holds per-agent environment variables.
	Env map[string]string `json:"env,omitempty"`
	// SecretName is the name of a K8s Secret to mount as envFrom.
	SecretName string `json:"secretName,omitempty"`
	// VolumeClaim is the name of an existing PVC to mount.
	VolumeClaim string `json:"volumeClaim,omitempty"`
	// VolumeMountPath is the container path where the PVC is mounted.
	VolumeMountPath string `json:"volumeMountPath,omitempty"`
}

// AgentResources holds optional resource requests and limits for the agent container.
type AgentResources struct {
	// Requests declares the minimum resources the container needs.
	Requests AgentResourceList `json:"requests,omitempty"`
	// Limits declares the maximum resources the container may use.
	Limits AgentResourceList `json:"limits,omitempty"`
}

// AgentResourceList describes a CPU / memory / ephemeral-storage triple
// used by both Requests and Limits on AgentResources.
type AgentResourceList struct {
	// CPU is the CPU resource value (e.g. "500m").
	CPU string `json:"cpu,omitempty"`
	// Memory is the memory resource value (e.g. "256Mi").
	Memory string `json:"memory,omitempty"`
	// EphemeralStorage is the ephemeral-storage resource value (e.g. "1Gi").
	EphemeralStorage string `json:"ephemeral-storage,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ConfigList is a list of Config resources.
type ConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	// Items is the list of Config resources.
	Items []Config `json:"items"`
}

// Equal returns true if this Config has the same spec as other.
func (a Config) Equal(other libk8s.Type) bool {
	switch o := other.(type) {
	case Config:
		return a.Spec.Equal(o.Spec)
	case *Config:
		return a.Spec.Equal(o.Spec)
	default:
		return false
	}
}

// Identifier returns a unique identifier for this Config.
func (a Config) Identifier() libk8s.Identifier {
	return libk8s.Identifier(libk8s.BuildName(a.Namespace, a.Name))
}

// Validate validates the Config spec.
func (a Config) Validate(ctx context.Context) error {
	return a.Spec.Validate(ctx)
}

// String returns the name of the Config.
func (a Config) String() string {
	return a.Name
}

// Equal returns true if the two ConfigSpec values are identical.
func (s ConfigSpec) Equal(o ConfigSpec) bool {
	return s.Assignee == o.Assignee &&
		s.Image == o.Image &&
		s.Heartbeat == o.Heartbeat &&
		s.SecretName == o.SecretName &&
		s.VolumeClaim == o.VolumeClaim &&
		s.VolumeMountPath == o.VolumeMountPath &&
		reflect.DeepEqual(s.Env, o.Env) &&
		reflect.DeepEqual(s.Resources, o.Resources)
}

// Validate validates the ConfigSpec fields.
func (s ConfigSpec) Validate(ctx context.Context) error {
	if s.Assignee == "" {
		return errors.Wrapf(ctx, validation.Error, "assignee is empty")
	}
	if s.Image == "" {
		return errors.Wrapf(ctx, validation.Error, "image is empty")
	}
	if s.Heartbeat == "" {
		return errors.Wrapf(ctx, validation.Error, "heartbeat is empty")
	}
	if s.VolumeClaim != "" && s.VolumeMountPath == "" {
		return errors.Wrapf(ctx, validation.Error, "VolumeMountPath required when VolumeClaim set")
	}
	return nil
}
