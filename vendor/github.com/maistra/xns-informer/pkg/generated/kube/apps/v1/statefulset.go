// Code generated by xns-informer-gen. DO NOT EDIT.

package v1

import (
	xnsinformers "github.com/maistra/xns-informer/pkg/informers"
	v1 "k8s.io/api/apps/v1"
	informers "k8s.io/client-go/informers/apps/v1"
	listers "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
)

type statefulSetInformer struct {
	informer cache.SharedIndexInformer
}

var _ informers.StatefulSetInformer = &statefulSetInformer{}

func NewStatefulSetInformer(f xnsinformers.SharedInformerFactory) informers.StatefulSetInformer {
	resource := v1.SchemeGroupVersion.WithResource("statefulsets")
	converter := xnsinformers.NewListWatchConverter(
		f.GetScheme(),
		&v1.StatefulSet{},
		&v1.StatefulSetList{},
	)

	informer := f.ForResource(resource, xnsinformers.ResourceOptions{
		ClusterScoped:      false,
		ListWatchConverter: converter,
	})

	return &statefulSetInformer{informer: informer.Informer()}
}

func (i *statefulSetInformer) Informer() cache.SharedIndexInformer {
	return i.informer
}

func (i *statefulSetInformer) Lister() listers.StatefulSetLister {
	return listers.NewStatefulSetLister(i.informer.GetIndexer())
}
