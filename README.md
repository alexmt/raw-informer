# raw-informer

A Kubernetes informer that stores custom resources as raw JSON bytes instead of parsing them into `map[string]interface{}`. Designed for sidecar controllers that only need a small subset of fields from a CRD they don't own.

**Results on production clusters with 1,000+ Argo CD Applications:** ~10x CPU reduction, ~5x memory reduction compared to an unstructured informer.

## The Problem

Sidecar controllers — controllers that piggyback on an existing CRD rather than owning one — are a common pattern in the Kubernetes ecosystem. [notifications-engine](https://github.com/argoproj/notifications-engine) is a typical example: it watches Argo CD `Application` objects and acts on annotations, but doesn't care about spec or status at all.

The natural implementation uses `*unstructured.Unstructured` to avoid coupling with the primary controller's generated types. The problem is that `*unstructured.Unstructured` allocates a deeply-nested `map[string]interface{}` for every object on every list and re-list. For a complex CRD with thousands of instances, a controller that only reads two annotations is paying a large allocation tax for data it will never use.

## The Solution

Store the full JSON as `[]byte`. Use [gjson](https://github.com/tidwall/gjson) to extract the specific fields you need by scanning the raw bytes directly — no map, no tree, no allocation beyond the bytes themselves.

The informer framework only requires `name`, `namespace`, `resourceVersion`, and `labels` to function. Everything else can stay as raw bytes until (and unless) your handler actually needs it.

## Install

```
go get github.com/alexmt/raw-informer
```

## Usage

```go
import (
    rawinformer "github.com/alexmt/raw-informer"
    "github.com/tidwall/gjson"
)

informer, err := rawinformer.NewRawObjectInformerForConfig(
    config,
    schema.GroupVersionResource{
        Group:    "argoproj.io",
        Version:  "v1alpha1",
        Resource: "applications",
    },
    "Application",
    "",             // all namespaces; pass a namespace to restrict
    10*time.Minute, // resync period
    nil,            // optional func(*metav1.ListOptions) to filter server-side
)
if err != nil {
    return err
}

informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
    AddFunc: func(obj interface{}) {
        raw := obj.(*rawinformer.RawObject)

        // Standard metadata is available directly
        fmt.Println(raw.GetName(), raw.GetNamespace())

        // Read any other field with gjson — no allocation, no full parse
        annotation := gjson.GetBytes(raw.Raw(), "metadata.annotations.my-annotation").String()

        // If you need a whole subtree, unmarshal only that part
        result := gjson.GetBytes(raw.Raw(), "status")
        var status MyStatus
        json.Unmarshal([]byte(result.Raw), &status)
    },
})

informer.Informer().Run(ctx.Done())
```

See the [`example/`](example/) directory for a complete annotation-watching controller.

### Starting from an existing Unstructured object

If you're working with an existing shared informer factory, convert without double-parsing:

```go
rawObj, err := rawinformer.NewRawObjectFromUnstructured(u)
```

## How It Works

`RawObject` implements `json.Unmarshaler`. When the Kubernetes REST client decodes a list or watch response, it calls `UnmarshalJSON` on each item. Instead of building a map, `UnmarshalJSON` copies the raw bytes and uses gjson to extract only the four fields the informer cache needs:

```go
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
    // ...
    return nil
}
```

The library registers `*RawObject` in a minimal custom scheme and uses `codecs.WithoutConversion()` as the negotiated serializer, so `*unstructured.Unstructured` is never allocated — not even transiently.

## When to Use This

Good fit:
- Sidecar/supplemental controllers that don't own the CRD
- Only a small subset of fields is needed per object
- High object count (hundreds to thousands) or high churn rate
- The CRD has a large or complex spec/status

Not needed:
- Primary controllers that use the full object
- Small or low-cardinality CRDs
