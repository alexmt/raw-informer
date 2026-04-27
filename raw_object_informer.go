package rawinformer

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// NewRawObjectInformerForConfig creates a GenericInformer backed by a REST client
// whose scheme maps the given GVK to *RawObject. The scheme JSON decoder calls
// RawObject.UnmarshalJSON directly on every list and watch event, so
// *unstructured.Unstructured is never allocated — including during re-lists.
func NewRawObjectInformerForConfig(
	config *rest.Config,
	gvr schema.GroupVersionResource,
	kind string,
	namespace string,
	resyncPeriod time.Duration,
	tweakListOptions func(*metav1.ListOptions),
) (informers.GenericInformer, error) {
	lw, err := newRawObjectListerWatcher(config, gvr, kind, namespace, tweakListOptions)
	if err != nil {
		return nil, err
	}
	inf := cache.NewSharedIndexInformer(lw, &RawObject{}, resyncPeriod, cache.Indexers{})
	return &rawObjectInformer{informer: inf, resource: gvr.GroupResource()}, nil
}

type rawObjectInformer struct {
	informer cache.SharedIndexInformer
	resource schema.GroupResource
}

func (i *rawObjectInformer) Informer() cache.SharedIndexInformer {
	return i.informer
}

func (i *rawObjectInformer) Lister() cache.GenericLister {
	return cache.NewGenericLister(i.informer.GetIndexer(), i.resource)
}

// RawObjectList is a runtime.Object list whose items are *RawObject.
// It is registered in the scheme so the JSON decoder populates Items
// via RawObject.UnmarshalJSON.
type RawObjectList struct {
	metav1.TypeMeta
	metav1.ListMeta
	Items []*RawObject `json:"items"`
}

func (l *RawObjectList) GetObjectKind() schema.ObjectKind { return schema.EmptyObjectKind }

func (l *RawObjectList) DeepCopyObject() runtime.Object {
	cp := &RawObjectList{
		TypeMeta: l.TypeMeta,
		ListMeta: l.ListMeta,
	}
	cp.Items = make([]*RawObject, len(l.Items))
	for i, item := range l.Items {
		cp.Items[i] = item.DeepCopyObject().(*RawObject)
	}
	return cp
}

// newRawObjectListerWatcher builds a ListerWatcher that deserializes list and watch
// responses directly into *RawObject / *RawObjectList, bypassing *unstructured.Unstructured.
//
// How it works:
//  1. A minimal scheme is created and the GVK pair (kind / kindList) is registered
//     against *RawObject and *RawObjectList. metav1 types (Status, WatchEvent, …) are
//     added so the codec can handle error and watch-event envelopes correctly.
//  2. A CodecFactory is built from that scheme. codecs.WithoutConversion() is used as
//     the NegotiatedSerializer so the REST client never attempts conversion between
//     API versions — it simply decodes whatever the server sends.
//  3. A typed REST client is configured for the target group/version. For core resources
//     (group == "") the base path is /api; for everything else it is /apis.
//  4. The resulting client is wired into rawObjectListerWatcher, which issues plain
//     GET requests for list and GET?watch=true for watch.
//
// Because *RawObject implements json.Unmarshaler, the scheme's JSON decoder calls
// UnmarshalJSON directly on each item — *unstructured.Unstructured is never allocated.
func newRawObjectListerWatcher(
	config *rest.Config,
	gvr schema.GroupVersionResource,
	kind string,
	namespace string,
	tweakListOptions func(*metav1.ListOptions),
) (cache.ListerWatcher, error) {
	httpClient, err := rest.HTTPClientFor(config)
	if err != nil {
		return nil, err
	}

	scheme := runtime.NewScheme()
	gv := gvr.GroupVersion()
	scheme.AddKnownTypeWithName(gv.WithKind(kind), &RawObject{})
	scheme.AddKnownTypeWithName(gv.WithKind(kind+"List"), &RawObjectList{})
	metav1.AddToGroupVersion(scheme, gv)

	codecs := serializer.NewCodecFactory(scheme)

	cfg := rest.CopyConfig(config)
	cfg.GroupVersion = &gv
	cfg.APIPath = "/apis"
	if gvr.Group == "" {
		cfg.APIPath = "/api"
	}
	cfg.NegotiatedSerializer = codecs.WithoutConversion()

	restClient, err := rest.RESTClientForConfigAndClient(cfg, httpClient)
	if err != nil {
		return nil, err
	}
	return &rawObjectListerWatcher{
		restClient:       restClient,
		resource:         gvr.Resource,
		namespace:        namespace,
		tweakListOptions: tweakListOptions,
	}, nil
}

type rawObjectListerWatcher struct {
	restClient       rest.Interface
	resource         string
	namespace        string
	tweakListOptions func(*metav1.ListOptions)
}

func (lw *rawObjectListerWatcher) List(opts metav1.ListOptions) (runtime.Object, error) {
	if lw.tweakListOptions != nil {
		lw.tweakListOptions(&opts)
	}
	list := &RawObjectList{}
	err := lw.restClient.Get().
		Namespace(lw.namespace).
		Resource(lw.resource).
		VersionedParams(&opts, metav1.ParameterCodec).
		Do(context.TODO()).
		Into(list)
	return list, err
}

func (lw *rawObjectListerWatcher) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	if lw.tweakListOptions != nil {
		lw.tweakListOptions(&opts)
	}
	opts.Watch = true
	return lw.restClient.Get().
		Namespace(lw.namespace).
		Resource(lw.resource).
		VersionedParams(&opts, metav1.ParameterCodec).
		Watch(context.TODO())
}
