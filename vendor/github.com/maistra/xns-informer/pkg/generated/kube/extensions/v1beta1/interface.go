// Code generated by xns-informer-gen. DO NOT EDIT.

package v1beta1

import (
	xnsinformers "github.com/maistra/xns-informer/pkg/informers"
	informers "k8s.io/client-go/informers/extensions/v1beta1"
)

type Interface interface {
	DaemonSets() informers.DaemonSetInformer
	Deployments() informers.DeploymentInformer
	Ingresses() informers.IngressInformer
	NetworkPolicies() informers.NetworkPolicyInformer
	PodSecurityPolicies() informers.PodSecurityPolicyInformer
	ReplicaSets() informers.ReplicaSetInformer
}

type version struct {
	factory xnsinformers.SharedInformerFactory
}

func New(factory xnsinformers.SharedInformerFactory) Interface {
	return &version{factory: factory}
}
func (v *version) DaemonSets() informers.DaemonSetInformer {
	return NewDaemonSetInformer(v.factory)
}
func (v *version) Deployments() informers.DeploymentInformer {
	return NewDeploymentInformer(v.factory)
}
func (v *version) Ingresses() informers.IngressInformer {
	return NewIngressInformer(v.factory)
}
func (v *version) NetworkPolicies() informers.NetworkPolicyInformer {
	return NewNetworkPolicyInformer(v.factory)
}
func (v *version) PodSecurityPolicies() informers.PodSecurityPolicyInformer {
	return NewPodSecurityPolicyInformer(v.factory)
}
func (v *version) ReplicaSets() informers.ReplicaSetInformer {
	return NewReplicaSetInformer(v.factory)
}
