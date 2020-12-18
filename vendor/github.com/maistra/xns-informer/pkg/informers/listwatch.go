package informers

import (
	"reflect"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

// ListWatchConverter allows you to wrap a ListWatch that uses the dynamic
// client and convert the returned unstructured objects to structured ones.
type ListWatchConverter struct {
	listType  reflect.Type
	objType   reflect.Type
	converter runtime.ObjectConvertor
}

// NewListWatchConverter returns a new ListWatchConverter that will use the
// given ObjectConvertor.  The `obj` and `list` parameters should be instances
// of the concrete types for the conversion, e.g. for ConfigMaps that would be:
// &v1.ConfigMap{} and &v1.ConfigMapList{}
func NewListWatchConverter(conv runtime.ObjectConvertor, obj, list runtime.Object) *ListWatchConverter {
	return &ListWatchConverter{
		listType:  reflect.ValueOf(list).Elem().Type(),
		objType:   reflect.ValueOf(obj).Elem().Type(),
		converter: conv,
	}
}

// NewObject returns a new instance of the concrete object type.
func (c *ListWatchConverter) NewObject() runtime.Object {
	return reflect.New(c.objType).Interface().(runtime.Object)
}

// NewList returns a new instance of the concrete list type.
func (c *ListWatchConverter) NewList() runtime.Object {
	return reflect.New(c.listType).Interface().(runtime.Object)
}

// Wrap wraps the given ListWatch and returns a version that converts objects to
// the configured concrete types.
func (c *ListWatchConverter) Wrap(lw *cache.ListWatch) *cache.ListWatch {
	return &cache.ListWatch{
		ListFunc:  c.lister(lw.ListFunc),
		WatchFunc: c.watcher(lw.WatchFunc),
	}
}

// lister returns a wrapped ListFunc.
func (c *ListWatchConverter) lister(lf cache.ListFunc) cache.ListFunc {
	return func(opts metav1.ListOptions) (runtime.Object, error) {
		list, err := lf(opts)
		if err != nil {
			return nil, err
		}

		var items []runtime.Object
		err = apimeta.EachListItem(list, func(obj runtime.Object) error {
			converted, err := c.convert(obj)
			if err != nil {
				return err
			}

			items = append(items, converted)
			return nil
		})
		if err != nil {
			return nil, err
		}

		newList := c.NewList()
		gvk := list.GetObjectKind().GroupVersionKind()
		newList.GetObjectKind().SetGroupVersionKind(gvk)

		if err := copyListMeta(list, newList); err != nil {
			return nil, err
		}

		if err := apimeta.SetList(newList, items); err != nil {
			return nil, err
		}

		return newList, nil
	}
}

// watcher returns a wrapped WatchFunc.
func (c *ListWatchConverter) watcher(wf cache.WatchFunc) cache.WatchFunc {
	return func(opts metav1.ListOptions) (watch.Interface, error) {
		watcher, err := wf(opts)
		if err != nil {
			return nil, err
		}

		filtered := watch.Filter(watcher, func(in watch.Event) (out watch.Event, keep bool) {
			obj, err := c.convert(in.Object)
			if err != nil {
				return watch.Event{}, false
			}

			out.Type = in.Type
			out.Object = obj

			return out, true
		})

		return filtered, nil
	}
}

// convert attempts to convert the given object to the configured type.
func (c *ListWatchConverter) convert(in runtime.Object) (runtime.Object, error) {
	var out runtime.Object

	if apimeta.IsListType(in) {
		out = c.NewList()
	} else {
		out = c.NewObject()
	}

	if err := c.converter.Convert(in, out, nil); err != nil {
		klog.Errorf("ListWatchConverter: %v", err)
		return nil, err
	}

	return out, nil
}

// TODO: There is almost certainly a better way to do this...
func copyListMeta(src, dest runtime.Object) error {
	srcMeta, err := apimeta.ListAccessor(src)
	if err != nil {
		return err
	}

	destMeta, err := apimeta.ListAccessor(src)
	if err != nil {
		return err
	}

	destMeta.SetResourceVersion(srcMeta.GetResourceVersion())
	destMeta.SetSelfLink(srcMeta.GetSelfLink())
	destMeta.SetContinue(srcMeta.GetContinue())
	destMeta.SetRemainingItemCount(srcMeta.GetRemainingItemCount())

	return nil
}
