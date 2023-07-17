// GENERATED FILE -- DO NOT EDIT
//

package kubetypes

import (
	"fmt"

	k8sioapiadmissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	k8sioapiappsv1 "k8s.io/api/apps/v1"
	k8sioapicertificatesv1 "k8s.io/api/certificates/v1"
	k8sioapicorev1 "k8s.io/api/core/v1"
	k8sioapidiscoveryv1 "k8s.io/api/discovery/v1"
	k8sioapinetworkingv1 "k8s.io/api/networking/v1"
	k8sioapiextensionsapiserverpkgapisapiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	sigsk8siogatewayapiapisv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	sigsk8siogatewayapiapisv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	istioioapiextensionsv1alpha1 "istio.io/api/extensions/v1alpha1"
	istioioapimeshv1alpha1 "istio.io/api/mesh/v1alpha1"
	istioioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istioioapinetworkingv1beta1 "istio.io/api/networking/v1beta1"
	istioioapisecurityv1beta1 "istio.io/api/security/v1beta1"
	istioioapitelemetryv1alpha1 "istio.io/api/telemetry/v1alpha1"
	apiistioioapiextensionsv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	apiistioioapinetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	apiistioioapinetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	apiistioioapisecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	apiistioioapitelemetryv1alpha1 "istio.io/client-go/pkg/apis/telemetry/v1alpha1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/ptr"
)

func GetGVK[T runtime.Object]() config.GroupVersionKind {
	switch any(ptr.Empty[T]()).(type) {
	case *istioioapisecurityv1beta1.AuthorizationPolicy:
		return gvk.AuthorizationPolicy
	case *apiistioioapisecurityv1beta1.AuthorizationPolicy:
		return gvk.AuthorizationPolicy
	case *k8sioapicertificatesv1.CertificateSigningRequest:
		return gvk.CertificateSigningRequest
	case *k8sioapicorev1.ConfigMap:
		return gvk.ConfigMap
	case *k8sioapiextensionsapiserverpkgapisapiextensionsv1.CustomResourceDefinition:
		return gvk.CustomResourceDefinition
	case *k8sioapiappsv1.Deployment:
		return gvk.Deployment
	case *istioioapinetworkingv1alpha3.DestinationRule:
		return gvk.DestinationRule
	case *apiistioioapinetworkingv1alpha3.DestinationRule:
		return gvk.DestinationRule
	case *k8sioapidiscoveryv1.EndpointSlice:
		return gvk.EndpointSlice
	case *k8sioapicorev1.Endpoints:
		return gvk.Endpoints
	case *istioioapinetworkingv1alpha3.EnvoyFilter:
		return gvk.EnvoyFilter
	case *apiistioioapinetworkingv1alpha3.EnvoyFilter:
		return gvk.EnvoyFilter
	case *sigsk8siogatewayapiapisv1alpha2.GRPCRoute:
		return gvk.GRPCRoute
	case *istioioapinetworkingv1alpha3.Gateway:
		return gvk.Gateway
	case *apiistioioapinetworkingv1alpha3.Gateway:
		return gvk.Gateway
	case *sigsk8siogatewayapiapisv1beta1.GatewayClass:
		return gvk.GatewayClass
	case *sigsk8siogatewayapiapisv1beta1.HTTPRoute:
		return gvk.HTTPRoute
	case *k8sioapinetworkingv1.Ingress:
		return gvk.Ingress
	case *k8sioapinetworkingv1.IngressClass:
		return gvk.IngressClass
	case *sigsk8siogatewayapiapisv1beta1.Gateway:
		return gvk.KubernetesGateway
	case *istioioapimeshv1alpha1.MeshConfig:
		return gvk.MeshConfig
	case *istioioapimeshv1alpha1.MeshNetworks:
		return gvk.MeshNetworks
	case *k8sioapiadmissionregistrationv1.MutatingWebhookConfiguration:
		return gvk.MutatingWebhookConfiguration
	case *k8sioapicorev1.Namespace:
		return gvk.Namespace
	case *k8sioapicorev1.Node:
		return gvk.Node
	case *istioioapisecurityv1beta1.PeerAuthentication:
		return gvk.PeerAuthentication
	case *apiistioioapisecurityv1beta1.PeerAuthentication:
		return gvk.PeerAuthentication
	case *k8sioapicorev1.Pod:
		return gvk.Pod
	case *istioioapinetworkingv1beta1.ProxyConfig:
		return gvk.ProxyConfig
	case *apiistioioapinetworkingv1beta1.ProxyConfig:
		return gvk.ProxyConfig
	case *sigsk8siogatewayapiapisv1beta1.ReferenceGrant:
		return gvk.ReferenceGrant
	case *istioioapisecurityv1beta1.RequestAuthentication:
		return gvk.RequestAuthentication
	case *apiistioioapisecurityv1beta1.RequestAuthentication:
		return gvk.RequestAuthentication
	case *k8sioapicorev1.Secret:
		return gvk.Secret
	case *k8sioapicorev1.Service:
		return gvk.Service
	case *k8sioapicorev1.ServiceAccount:
		return gvk.ServiceAccount
	case *istioioapinetworkingv1alpha3.ServiceEntry:
		return gvk.ServiceEntry
	case *apiistioioapinetworkingv1alpha3.ServiceEntry:
		return gvk.ServiceEntry
	case *istioioapinetworkingv1alpha3.Sidecar:
		return gvk.Sidecar
	case *apiistioioapinetworkingv1alpha3.Sidecar:
		return gvk.Sidecar
	case *sigsk8siogatewayapiapisv1alpha2.TCPRoute:
		return gvk.TCPRoute
	case *sigsk8siogatewayapiapisv1alpha2.TLSRoute:
		return gvk.TLSRoute
	case *istioioapitelemetryv1alpha1.Telemetry:
		return gvk.Telemetry
	case *apiistioioapitelemetryv1alpha1.Telemetry:
		return gvk.Telemetry
	case *sigsk8siogatewayapiapisv1alpha2.UDPRoute:
		return gvk.UDPRoute
	case *k8sioapiadmissionregistrationv1.ValidatingWebhookConfiguration:
		return gvk.ValidatingWebhookConfiguration
	case *istioioapinetworkingv1alpha3.VirtualService:
		return gvk.VirtualService
	case *apiistioioapinetworkingv1alpha3.VirtualService:
		return gvk.VirtualService
	case *istioioapiextensionsv1alpha1.WasmPlugin:
		return gvk.WasmPlugin
	case *apiistioioapiextensionsv1alpha1.WasmPlugin:
		return gvk.WasmPlugin
	case *istioioapinetworkingv1alpha3.WorkloadEntry:
		return gvk.WorkloadEntry
	case *apiistioioapinetworkingv1alpha3.WorkloadEntry:
		return gvk.WorkloadEntry
	case *istioioapinetworkingv1alpha3.WorkloadGroup:
		return gvk.WorkloadGroup
	case *apiistioioapinetworkingv1alpha3.WorkloadGroup:
		return gvk.WorkloadGroup
	default:
		panic(fmt.Sprintf("Unknown type %T", ptr.Empty[T]()))
	}
}
