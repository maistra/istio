// Code generated by xns-informer-gen. DO NOT EDIT.

package v1beta1

import (
	xnsinformers "github.com/maistra/xns-informer/pkg/informers"
	v1beta1 "k8s.io/api/rbac/v1beta1"
	informers "k8s.io/client-go/informers/rbac/v1beta1"
	listers "k8s.io/client-go/listers/rbac/v1beta1"
	"k8s.io/client-go/tools/cache"
)

type clusterRoleBindingInformer struct {
	informer cache.SharedIndexInformer
}

var _ informers.ClusterRoleBindingInformer = &clusterRoleBindingInformer{}

func NewClusterRoleBindingInformer(f xnsinformers.SharedInformerFactory) informers.ClusterRoleBindingInformer {
	resource := v1beta1.SchemeGroupVersion.WithResource("clusterrolebindings")
	converter := xnsinformers.NewListWatchConverter(
		f.GetScheme(),
		&v1beta1.ClusterRoleBinding{},
		&v1beta1.ClusterRoleBindingList{},
	)

	informer := f.ForResource(resource, xnsinformers.ResourceOptions{
		ClusterScoped:      true,
		ListWatchConverter: converter,
	})

	return &clusterRoleBindingInformer{informer: informer.Informer()}
}

func (i *clusterRoleBindingInformer) Informer() cache.SharedIndexInformer {
	return i.informer
}

func (i *clusterRoleBindingInformer) Lister() listers.ClusterRoleBindingLister {
	return listers.NewClusterRoleBindingLister(i.informer.GetIndexer())
}
