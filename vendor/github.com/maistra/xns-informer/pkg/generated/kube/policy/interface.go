// Code generated by xns-informer-gen. DO NOT EDIT.

package policy

import (
	v1beta1 "github.com/maistra/xns-informer/pkg/generated/kube/policy/v1beta1"
	xnsinformers "github.com/maistra/xns-informer/pkg/informers"
)

type Interface interface {
	V1beta1() v1beta1.Interface
}

type group struct {
	factory xnsinformers.SharedInformerFactory
}

func New(factory xnsinformers.SharedInformerFactory) Interface {
	return &group{factory: factory}
}

// V1beta1 returns a new v1beta1.Interface.
func (g *group) V1beta1() v1beta1.Interface {
	return v1beta1.New(g.factory)
}
