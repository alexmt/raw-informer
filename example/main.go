// This example demonstrates how to use raw-informer to watch Argo CD Application
// objects and react to annotation changes without fully parsing each CR into a map.
//
// The controller reads annotations from the raw JSON using gjson and ignores all
// other fields — status, spec, etc. — that it doesn't need.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	rawinformer "github.com/alexmt/raw-informer"
	"github.com/tidwall/gjson"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var applicationGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

const annotationKey = "notifications.argoproj.io/subscribe"

func main() {
	config, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	informer, err := rawinformer.NewRawObjectInformerForConfig(
		config,
		applicationGVR,
		"Application",
		"", // all namespaces
		10*time.Minute,
		func(opts *metav1.ListOptions) {
			// optionally filter server-side, e.g. by label selector
		},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create informer: %v\n", err)
		os.Exit(1)
	}

	_, _ = informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			handleApp("ADD", obj)
		},
		UpdateFunc: func(_, newObj interface{}) {
			handleApp("UPDATE", newObj)
		},
		DeleteFunc: func(obj interface{}) {
			handleApp("DELETE", obj)
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	informer.Informer().Run(ctx.Done())
}

// handleApp processes an event. It reads only the annotation it cares about
// from the raw JSON — the rest of the CR (spec, status, etc.) is never touched.
func handleApp(event string, obj interface{}) {
	raw, ok := obj.(*rawinformer.RawObject)
	if !ok {
		return
	}

	// Use gjson to read a single annotation from the raw JSON bytes.
	// No map allocation, no full unmarshal.
	annotation := gjson.GetBytes(raw.Raw(), "metadata.annotations."+escapeGjsonKey(annotationKey)).String()

	fmt.Printf("[%s] %s/%s  annotation=%q\n",
		event, raw.GetNamespace(), raw.GetName(), annotation)
}

// If you need richer access, unmarshal only the part you care about.
func parseStatus(raw *rawinformer.RawObject) string {
	var status struct {
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
	}
	result := gjson.GetBytes(raw.Raw(), "status")
	if result.Raw == "" {
		return ""
	}
	_ = json.Unmarshal([]byte(result.Raw), &status)
	return status.Health.Status
}

func escapeGjsonKey(k string) string {
	// gjson uses dots as path separators; annotation keys contain dots and slashes.
	// Wrap in a literal path component using backtick syntax.
	return "`" + k + "`"
}

func loadConfig() (*rest.Config, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.ExpandEnv("$HOME/.kube/config")
	}
	if _, err := os.Stat(kubeconfig); err == nil {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}
