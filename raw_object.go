package rawinformer

import (
	"encoding/json"

	"github.com/tidwall/gjson"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// RawObject stores a Kubernetes object as pre-serialized JSON bytes alongside
// the metadata fields needed for filtering, avoiding repeated reflection-based encoding on list/watch.
//
// MarshalJSON returns the stored raw bytes directly. UnmarshalJSON stores raw bytes and uses
// gjson for targeted field extraction — the full document is never decoded into a Go map.
type RawObject struct {
	metav1.ObjectMeta
	raw json.RawMessage
}

func (r *RawObject) SetData(raw []byte) {
	r.raw = raw
}

// MarshalJSON returns the pre-serialized raw bytes, bypassing reflection-based encoding.
func (r *RawObject) MarshalJSON() ([]byte, error) {
	return r.raw, nil
}

// UnmarshalJSON stores raw bytes and uses gjson for targeted field extraction.
// Only the fields actually needed are read; the rest of the document is skipped.
func (r *RawObject) UnmarshalJSON(data []byte) error {
	r.raw = make(json.RawMessage, len(data))
	copy(r.raw, data)

	results := gjson.GetManyBytes(data,
		"metadata.name",
		"metadata.namespace",
		"metadata.resourceVersion",
		"metadata.labels",
	)
	r.Name = results[0].Str
	r.Namespace = results[1].Str
	r.ResourceVersion = results[2].Str
	if raw := results[3].Raw; raw != "" {
		if err := json.Unmarshal([]byte(raw), &r.Labels); err != nil {
			return err
		}
	}
	return nil
}

// GetObjectKind implements runtime.Object.
func (r *RawObject) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

// DeepCopyObject implements runtime.Object.
func (r *RawObject) DeepCopyObject() runtime.Object {
	cp := RawObject{
		ObjectMeta: *r.DeepCopy(),
	}
	if r.raw != nil {
		cp.raw = make(json.RawMessage, len(r.raw))
		copy(cp.raw, r.raw)
	}
	return &cp
}

// Raw returns the pre-serialized JSON bytes of the object.
func (r *RawObject) Raw() []byte {
	return r.raw
}

// NewRawObjectFromUnstructured creates a RawObject from an Unstructured object
// serializing it once and extracting metadata fields directly from the already-parsed getters.
func NewRawObjectFromUnstructured(u *unstructured.Unstructured) (*RawObject, error) {
	b, err := u.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return &RawObject{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       u.GetNamespace(),
			Name:            u.GetName(),
			ResourceVersion: u.GetResourceVersion(),
			Labels:          u.GetLabels(),
		},
		raw: b,
	}, nil
}
