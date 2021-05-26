// Copyright Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by main. DO NOT EDIT.

package v1alpha1

import (
	"context"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
	scheme "maistra.io/api/client/versioned/scheme"
	v1alpha1 "maistra.io/api/core/v1alpha1"
)

// FederationStatusesGetter has a method to return a FederationStatusInterface.
// A group's client should implement this interface.
type FederationStatusesGetter interface {
	FederationStatuses(namespace string) FederationStatusInterface
}

// FederationStatusInterface has methods to work with FederationStatus resources.
type FederationStatusInterface interface {
	Create(ctx context.Context, federationStatus *v1alpha1.FederationStatus, opts v1.CreateOptions) (*v1alpha1.FederationStatus, error)
	Update(ctx context.Context, federationStatus *v1alpha1.FederationStatus, opts v1.UpdateOptions) (*v1alpha1.FederationStatus, error)
	UpdateStatus(ctx context.Context, federationStatus *v1alpha1.FederationStatus, opts v1.UpdateOptions) (*v1alpha1.FederationStatus, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.FederationStatus, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.FederationStatusList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.FederationStatus, err error)
	FederationStatusExpansion
}

// federationStatuses implements FederationStatusInterface
type federationStatuses struct {
	client rest.Interface
	ns     string
}

// newFederationStatuses returns a FederationStatuses
func newFederationStatuses(c *CoreV1alpha1Client, namespace string) *federationStatuses {
	return &federationStatuses{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the federationStatus, and returns the corresponding federationStatus object, and an error if there is any.
func (c *federationStatuses) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.FederationStatus, err error) {
	result = &v1alpha1.FederationStatus{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("federationstatuses").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of FederationStatuses that match those selectors.
func (c *federationStatuses) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.FederationStatusList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1alpha1.FederationStatusList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("federationstatuses").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested federationStatuses.
func (c *federationStatuses) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("federationstatuses").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a federationStatus and creates it.  Returns the server's representation of the federationStatus, and an error, if there is any.
func (c *federationStatuses) Create(ctx context.Context, federationStatus *v1alpha1.FederationStatus, opts v1.CreateOptions) (result *v1alpha1.FederationStatus, err error) {
	result = &v1alpha1.FederationStatus{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("federationstatuses").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(federationStatus).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a federationStatus and updates it. Returns the server's representation of the federationStatus, and an error, if there is any.
func (c *federationStatuses) Update(ctx context.Context, federationStatus *v1alpha1.FederationStatus, opts v1.UpdateOptions) (result *v1alpha1.FederationStatus, err error) {
	result = &v1alpha1.FederationStatus{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("federationstatuses").
		Name(federationStatus.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(federationStatus).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *federationStatuses) UpdateStatus(ctx context.Context, federationStatus *v1alpha1.FederationStatus, opts v1.UpdateOptions) (result *v1alpha1.FederationStatus, err error) {
	result = &v1alpha1.FederationStatus{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("federationstatuses").
		Name(federationStatus.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(federationStatus).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the federationStatus and deletes it. Returns an error if one occurs.
func (c *federationStatuses) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("federationstatuses").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *federationStatuses) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("federationstatuses").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched federationStatus.
func (c *federationStatuses) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.FederationStatus, err error) {
	result = &v1alpha1.FederationStatus{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("federationstatuses").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
