// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	agents "github.com/bborbe/agent/task/executor/k8s/apis/agents.bborbe.dev"
)

// SchemeGroupVersion is the group and version for AgentConfig resources.
var SchemeGroupVersion = schema.GroupVersion{Group: agents.GroupName, Version: "v1"}

// SchemeBuilder is used to register types with the scheme.
var SchemeBuilder runtime.SchemeBuilder

var localSchemeBuilder = &SchemeBuilder

// AddToScheme adds AgentConfig types to the scheme.
var AddToScheme = localSchemeBuilder.AddToScheme

func init() {
	localSchemeBuilder.Register(addKnownTypes)
}

// Resource takes an unqualified resource and returns a Group-qualified GroupResource.
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&AgentConfig{},
		&AgentConfigList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
