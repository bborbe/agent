// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"

	"github.com/bborbe/errors"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

//counterfeiter:generate -o ../mocks/k8s_connector.go --fake-name FakeK8sConnector . K8sConnector

// K8sConnector installs the AgentConfig CRD and starts an informer.
type K8sConnector interface {
	SetupCustomResourceDefinition(ctx context.Context) error
	Listen(ctx context.Context, namespace string, handler cache.ResourceEventHandler) error
}

// CRDClientBuilder constructs the apiextensions clientset from a rest.Config.
// Injected to allow fake clientsets in tests.
type CRDClientBuilder func(*rest.Config) (apiextensionsclient.Interface, error)

// DefaultCRDClientBuilder wraps apiextensionsclient.NewForConfig to satisfy CRDClientBuilder.
func DefaultCRDClientBuilder(c *rest.Config) (apiextensionsclient.Interface, error) {
	return apiextensionsclient.NewForConfig(c)
}

// NewK8sConnector returns a K8sConnector using the given rest config and builder.
// Production callers pass DefaultCRDClientBuilder as the builder.
func NewK8sConnector(config *rest.Config, crdBuilder CRDClientBuilder) K8sConnector {
	return &k8sConnector{config: config, crdBuilder: crdBuilder}
}

type k8sConnector struct {
	config     *rest.Config
	crdBuilder CRDClientBuilder
}

// SetupCustomResourceDefinition installs or updates the AgentConfig CRD on the cluster.
func (c *k8sConnector) SetupCustomResourceDefinition(ctx context.Context) error {
	clientset, err := c.crdBuilder(c.config)
	if err != nil {
		return errors.Wrapf(ctx, err, "build apiextensions clientset")
	}
	crds := clientset.ApiextensionsV1().CustomResourceDefinitions()
	existing, err := crds.Get(ctx, "agentconfigs.agents.bborbe.dev", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		crd := &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: "agentconfigs.agents.bborbe.dev"},
			Spec:       desiredCRDSpec(),
		}
		if _, err := crds.Create(ctx, crd, metav1.CreateOptions{}); err != nil {
			return errors.Wrapf(ctx, err, "create CRD")
		}
		return nil
	}
	if err != nil {
		return errors.Wrapf(ctx, err, "get CRD")
	}
	existing.Spec = desiredCRDSpec()
	if _, err := crds.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return errors.Wrapf(ctx, err, "update CRD")
	}
	return nil
}

// Listen is not yet wired — see spec 007 prompt 2.
// TODO(spec-007-prompt-2): wire SharedInformerFactory
func (c *k8sConnector) Listen(
	ctx context.Context,
	_ string,
	_ cache.ResourceEventHandler,
) error {
	return errors.New(ctx, "Listen not yet wired — see spec 007 prompt 2")
}

func desiredCRDSpec() apiextensionsv1.CustomResourceDefinitionSpec {
	minLen := int64(1)
	return apiextensionsv1.CustomResourceDefinitionSpec{
		Group: "agents.bborbe.dev",
		Names: apiextensionsv1.CustomResourceDefinitionNames{
			Kind:       "AgentConfig",
			ListKind:   "AgentConfigList",
			Plural:     "agentconfigs",
			Singular:   "agentconfig",
			ShortNames: []string{"ac"},
		},
		Scope: apiextensionsv1.NamespaceScoped,
		Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
			{
				Name:    "v1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"spec": {
								Type:     "object",
								Required: []string{"assignee", "image", "heartbeat"},
								Properties: map[string]apiextensionsv1.JSONSchemaProps{
									"assignee": {
										Type:      "string",
										MinLength: &minLen,
									},
									"image": {
										Type:      "string",
										MinLength: &minLen,
									},
									"heartbeat": {
										Type:    "string",
										Pattern: "^[0-9]+(s|m|h)$",
									},
									"resources": {
										Type: "object",
										Properties: map[string]apiextensionsv1.JSONSchemaProps{
											"cpu":               {Type: "string"},
											"memory":            {Type: "string"},
											"ephemeral-storage": {Type: "string"},
										},
									},
									"env": {
										Type: "object",
										AdditionalProperties: &apiextensionsv1.JSONSchemaPropsOrBool{
											Schema: &apiextensionsv1.JSONSchemaProps{
												Type: "string",
											},
										},
									},
									"secretName":      {Type: "string"},
									"volumeClaim":     {Type: "string"},
									"volumeMountPath": {Type: "string"},
								},
							},
						},
					},
				},
			},
		},
		PreserveUnknownFields: false,
	}
}
