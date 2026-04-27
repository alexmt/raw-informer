package rawinformer

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRawObjectMarshalJSON(t *testing.T) {
	raw := json.RawMessage(`{"apiVersion":"argoproj.io/v1alpha1","kind":"Application"}`)
	app := &RawObject{raw: raw}

	b, err := app.MarshalJSON()
	require.NoError(t, err)
	assert.Equal(t, []byte(raw), b)
}

func TestRawObjectUnmarshalJSON(t *testing.T) {
	data := []byte(`{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind": "Application",
		"metadata": {
			"name": "my-app",
			"namespace": "argocd",
			"resourceVersion": "12345",
			"labels": {"cluster": "prod", "env": "production"}
		},
		"spec": {"project": "default"}
	}`)

	var app RawObject
	require.NoError(t, json.Unmarshal(data, &app))

	assert.Equal(t, data, []byte(app.raw))
	assert.Equal(t, "my-app", app.GetName())
	assert.Equal(t, "argocd", app.GetNamespace())
	assert.Equal(t, "12345", app.GetResourceVersion())
	assert.Equal(t, map[string]string{"cluster": "prod", "env": "production"}, app.GetLabels())
}

func TestRawObjectDeepCopyObject(t *testing.T) {
	app := &RawObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-app",
			Namespace: "argocd",
			Labels:    map[string]string{"k": "v"},
		},
		raw: json.RawMessage(`{"kind":"Application"}`),
	}

	cp := app.DeepCopyObject().(*RawObject)

	assert.Equal(t, app.GetName(), cp.GetName())
	assert.Equal(t, app.GetNamespace(), cp.GetNamespace())
	assert.Equal(t, app.GetLabels(), cp.GetLabels())
	assert.Equal(t, []byte(app.raw), []byte(cp.raw))

	// mutations to the copy must not affect the original
	cp.raw[0] = 'X'
	assert.NotEqual(t, app.raw[0], cp.raw[0])
	cp.Labels["k"] = "changed"
	assert.Equal(t, "v", app.GetLabels()["k"])
}

func TestNewRawObjectFromUnstructured(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Application",
			"metadata": map[string]interface{}{
				"name":            "my-app",
				"namespace":       "argocd",
				"resourceVersion": "42",
				"labels":          map[string]interface{}{"cluster": "staging"},
			},
		},
	}

	app, err := NewRawObjectFromUnstructured(u)
	require.NoError(t, err)

	assert.Equal(t, "my-app", app.GetName())
	assert.Equal(t, "argocd", app.GetNamespace())
	assert.Equal(t, "42", app.GetResourceVersion())
	assert.Equal(t, map[string]string{"cluster": "staging"}, app.GetLabels())

	var roundTrip map[string]interface{}
	require.NoError(t, json.Unmarshal(app.raw, &roundTrip))
	assert.Equal(t, "Application", roundTrip["kind"])
}
