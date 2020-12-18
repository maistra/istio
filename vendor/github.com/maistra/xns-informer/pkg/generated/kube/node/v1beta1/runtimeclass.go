// Code generated by xns-informer-gen. DO NOT EDIT.

package v1beta1

import (
	xnsinformers "github.com/maistra/xns-informer/pkg/informers"
	v1beta1 "k8s.io/api/node/v1beta1"
	informers "k8s.io/client-go/informers/node/v1beta1"
	listers "k8s.io/client-go/listers/node/v1beta1"
	"k8s.io/client-go/tools/cache"
)

type runtimeClassInformer struct {
	informer cache.SharedIndexInformer
}

var _ informers.RuntimeClassInformer = &runtimeClassInformer{}

func NewRuntimeClassInformer(f xnsinformers.SharedInformerFactory) informers.RuntimeClassInformer {
	resource := v1beta1.SchemeGroupVersion.WithResource("runtimeclasses")
	converter := xnsinformers.NewListWatchConverter(
		f.GetScheme(),
		&v1beta1.RuntimeClass{},
		&v1beta1.RuntimeClassList{},
	)

	informer := f.ForResource(resource, xnsinformers.ResourceOptions{
		ClusterScoped:      true,
		ListWatchConverter: converter,
	})

	return &runtimeClassInformer{informer: informer.Informer()}
}

func (i *runtimeClassInformer) Informer() cache.SharedIndexInformer {
	return i.informer
}

func (i *runtimeClassInformer) Lister() listers.RuntimeClassLister {
	return listers.NewRuntimeClassLister(i.informer.GetIndexer())
}
