package model

import (
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha1"
)

// ExtensionWrapper is a wrapper around extensions
type ExtensionWrapper struct {
	Name             string
	WorkloadSelector labels.Instance
	Config           string
	Image            string
	FilterURL        string
	SHA256           string
	Phase            v1alpha1.FilterPhase
	Priority         int
}

func ToWrapper(extension *v1alpha1.ServiceMeshExtension) *ExtensionWrapper {
	return &ExtensionWrapper{
		Name:             extension.Name,
		WorkloadSelector: extension.Spec.WorkloadSelector.Labels,
		Config:           extension.Spec.Config,
		Image:            extension.Spec.Image,
		FilterURL:        extension.Status.Deployment.URL,
		SHA256:           extension.Status.Deployment.SHA256,
		Phase:            extension.Status.Phase,
		Priority:         extension.Status.Priority,
	}
}
