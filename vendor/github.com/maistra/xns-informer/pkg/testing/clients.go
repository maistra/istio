package testing

import (
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/testing"
	serviceapisfake "sigs.k8s.io/service-apis/pkg/client/clientset/versioned/fake"
)

type FakeClients struct {
	Kubernetes  *kubefake.Clientset
	Istio       *istiofake.Clientset
	ServiceAPIs *serviceapisfake.Clientset
	Dynamic     *dynamicfake.FakeDynamicClient
}

// NewFakeClients returns a new fake typed client and a new fake dynamic client
// for testing.  The typed client is configured to reflect any writes into the
// dynamic client's store, converting them to unstructured objects.  This is a
// one-way operation.  Writes made via the dynamic client are not readable from
// the typed client.
func NewFakeClients(s *runtime.Scheme, objects ...runtime.Object) (*FakeClients, error) {
	// Register everything with the scheme.
	if err := kubefake.AddToScheme(s); err != nil {
		return nil, err
	}

	if err := istiofake.AddToScheme(s); err != nil {
		return nil, err
	}

	if err := serviceapisfake.AddToScheme(s); err != nil {
		return nil, err
	}

	// TODO: This may be unnecessary.  The dynamic fake does conversion as well.
	unstructuredObjects, err := ObjectsToUnstructured(s, objects...)
	if err != nil {
		return nil, err
	}

	kubeClient := kubefake.NewSimpleClientset(objects...)
	istioClient := istiofake.NewSimpleClientset()
	serviceClient := serviceapisfake.NewSimpleClientset()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(s, unstructuredObjects...)

	// Reflect any writes done via the typed client set into the dynamic
	// client's store after converting them to unstructured objects.
	kubeClient.PrependReactor("*", "*", UnstructuredObjectReflector(s, &dynamicClient.Fake))
	istioClient.PrependReactor("*", "*", UnstructuredObjectReflector(s, &dynamicClient.Fake))
	serviceClient.PrependReactor("*", "*", UnstructuredObjectReflector(s, &dynamicClient.Fake))

	clients := &FakeClients{
		Kubernetes:  kubeClient,
		Istio:       istioClient,
		ServiceAPIs: serviceClient,
		Dynamic:     dynamicClient,
	}

	return clients, nil
}

// ObjectsToUnstructured uses the given runtime.ObjectConvertor to map each
// input object into an unstructured object.
func ObjectsToUnstructured(conv runtime.ObjectConvertor, objects ...runtime.Object) ([]runtime.Object, error) {
	unstructuredObjects := make([]runtime.Object, len(objects))

	for i, obj := range objects {
		u := &unstructured.Unstructured{}
		if err := conv.Convert(obj, u, nil); err != nil {
			return nil, err
		}

		unstructuredObjects[i] = u
	}

	return unstructuredObjects, nil
}

// UnstructuredObjectReflector returns a new testing.ReactionFunc meant to be
// inserted at the front of a fake client's reactor chain.  It will reflect any
// write actions to the given testing.Fake after having converted the associated
// object to an unstructured.  The ReactionFunc's first return value is always
// false to indicate that the action was not handled and the client should
// proceed down its reactor chain.  You can use this to reflect writes performed
// via typed fake client into the object tracker associated with a fake dynamic
// client.
func UnstructuredObjectReflector(conv runtime.ObjectConvertor, f *testing.Fake) testing.ReactionFunc {
	return func(action testing.Action) (bool, runtime.Object, error) {
		var err error
		action = action.DeepCopy()

		switch action := action.(type) {

		case testing.CreateActionImpl:
			u := &unstructured.Unstructured{}
			if err := conv.Convert(action.GetObject(), u, nil); err != nil {
				return false, nil, err
			}
			action.Object = u
			_, err = f.Invokes(action, nil)

		case testing.UpdateActionImpl:
			u := &unstructured.Unstructured{}
			if err := conv.Convert(action.GetObject(), u, nil); err != nil {
				return false, nil, err
			}

			action.Object = u
			_, err = f.Invokes(action, nil)

		case testing.DeleteActionImpl:
			_, err = f.Invokes(action, nil)

		case testing.PatchActionImpl:
			_, err = f.Invokes(action, nil)
		}

		return false, nil, err
	}
}
