package applier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

// ManifestApplier applies any Kubernetes manifest (like kubectl apply)
type ManifestApplier struct {
	clientset     *kubernetes.Clientset
	dynamicClient dynamic.Interface
	discovery     discovery.DiscoveryInterface
	mapper        meta.RESTMapper
	namespace     string
	logger        *logrus.Logger
	restConfig    *rest.Config
}

// NewManifestApplier creates a new manifest applier
func NewManifestApplier(config *rest.Config, namespace string) (*ManifestApplier, error) {
	if namespace == "" {
		namespace = "default"
	}

	// Create clientset for standard resources
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	// Create dynamic client for any resource type
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Create discovery client for API discovery
	discoveryClient := clientset.Discovery()

	// Create REST mapper for resource mapping
	gr, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get API group resources: %w", err)
	}
	mapper := restmapper.NewDiscoveryRESTMapper(gr)

	return &ManifestApplier{
		clientset:     clientset,
		dynamicClient: dynamicClient,
		discovery:     discoveryClient,
		mapper:        mapper,
		namespace:     namespace,
		logger:        logrus.New(),
		restConfig:    config,
	}, nil
}

// ApplyYAML applies raw YAML content to Kubernetes (handles multiple documents)
func (a *ManifestApplier) ApplyYAML(ctx context.Context, yamlContent []byte) (*ApplyResult, error) {
	result := &ApplyResult{
		AppliedResources: []AppliedResource{},
		Errors:          []string{},
	}

	// Split YAML into multiple documents
	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(yamlContent), 4096)

	for {
		var rawObj runtime.RawExtension
		if err := decoder.Decode(&rawObj); err != nil {
			if err == io.EOF {
				break
			}
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to decode YAML: %v", err))
			continue
		}

		// Skip empty documents
		if len(rawObj.Raw) == 0 {
			continue
		}

		// Apply single resource
		resource, err := a.applySingleResource(ctx, rawObj.Raw)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			result.Failed = true
		} else {
			result.AppliedResources = append(result.AppliedResources, *resource)
		}
	}

	if len(result.AppliedResources) == 0 && len(result.Errors) > 0 {
		return result, fmt.Errorf("failed to apply any resources")
	}

	return result, nil
}

// applySingleResource applies a single Kubernetes resource
func (a *ManifestApplier) applySingleResource(ctx context.Context, resourceYAML []byte) (*AppliedResource, error) {
	obj := &unstructured.Unstructured{}
	dec := yamlserializer.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	_, _, err := dec.Decode(resourceYAML, nil, obj)
	if err != nil {
		return nil, fmt.Errorf("failed to decode resource: %w", err)
	}

	gvk := obj.GroupVersionKind()
	mapping, err := a.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to get REST mapping for %s: %w", gvk, err)
	}

	var dr dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if obj.GetNamespace() == "" {
			obj.SetNamespace(a.namespace)
		}
		dr = a.dynamicClient.Resource(mapping.Resource).Namespace(obj.GetNamespace())
	} else {
		dr = a.dynamicClient.Resource(mapping.Resource)
	}

	// Try to get existing resource
	existing, err := dr.Get(ctx, obj.GetName(), metav1.GetOptions{})

	var operation string
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new resource
			a.logger.WithFields(logrus.Fields{
				"kind":      obj.GetKind(),
				"name":      obj.GetName(),
				"namespace": obj.GetNamespace(),
			}).Info("Creating resource")

			created, err := dr.Create(ctx, obj, metav1.CreateOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to create %s/%s: %w", obj.GetKind(), obj.GetName(), err)
			}
			obj = created
			operation = "created"
		} else {
			return nil, fmt.Errorf("failed to get existing %s/%s: %w", obj.GetKind(), obj.GetName(), err)
		}
	} else {
		// Update existing resource
		a.logger.WithFields(logrus.Fields{
			"kind":      obj.GetKind(),
			"name":      obj.GetName(),
			"namespace": obj.GetNamespace(),
		}).Info("Updating resource")

		// Preserve existing metadata
		obj.SetResourceVersion(existing.GetResourceVersion())
		obj.SetUID(existing.GetUID())

		updated, err := dr.Update(ctx, obj, metav1.UpdateOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to update %s/%s: %w", obj.GetKind(), obj.GetName(), err)
		}
		obj = updated
		operation = "updated"
	}

	return &AppliedResource{
		Kind:      obj.GetKind(),
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		Operation: operation,
		UID:       string(obj.GetUID()),
	}, nil
}

// DeleteYAML deletes resources defined in YAML
func (a *ManifestApplier) DeleteYAML(ctx context.Context, yamlContent []byte) error {
	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(yamlContent), 4096)

	for {
		var rawObj runtime.RawExtension
		if err := decoder.Decode(&rawObj); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to decode YAML: %w", err)
		}

		if len(rawObj.Raw) == 0 {
			continue
		}

		if err := a.deleteSingleResource(ctx, rawObj.Raw); err != nil {
			a.logger.WithError(err).Warn("Failed to delete resource")
			// Continue deleting other resources
		}
	}

	return nil
}

// deleteSingleResource deletes a single Kubernetes resource
func (a *ManifestApplier) deleteSingleResource(ctx context.Context, resourceYAML []byte) error {
	obj := &unstructured.Unstructured{}
	dec := yamlserializer.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	_, _, err := dec.Decode(resourceYAML, nil, obj)
	if err != nil {
		return fmt.Errorf("failed to decode resource: %w", err)
	}

	gvk := obj.GroupVersionKind()
	mapping, err := a.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("failed to get REST mapping: %w", err)
	}

	var dr dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if obj.GetNamespace() == "" {
			obj.SetNamespace(a.namespace)
		}
		dr = a.dynamicClient.Resource(mapping.Resource).Namespace(obj.GetNamespace())
	} else {
		dr = a.dynamicClient.Resource(mapping.Resource)
	}

	a.logger.WithFields(logrus.Fields{
		"kind":      obj.GetKind(),
		"name":      obj.GetName(),
		"namespace": obj.GetNamespace(),
	}).Info("Deleting resource")

	err = dr.Delete(ctx, obj.GetName(), metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete %s/%s: %w", obj.GetKind(), obj.GetName(), err)
	}

	return nil
}


// GetResourceStatus gets the status of a resource
func (a *ManifestApplier) GetResourceStatus(ctx context.Context, kind, name, namespace string) (map[string]interface{}, error) {
	// This is a simplified version - you'd need to properly map kind to GVR
	gvk := schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    kind,
	}

	mapping, err := a.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		// Try core v1 resources
		gvk.Group = ""
		mapping, err = a.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to get REST mapping: %w", err)
		}
	}

	var dr dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if namespace == "" {
			namespace = a.namespace
		}
		dr = a.dynamicClient.Resource(mapping.Resource).Namespace(namespace)
	} else {
		dr = a.dynamicClient.Resource(mapping.Resource)
	}

	obj, err := dr.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	status, found, err := unstructured.NestedMap(obj.Object, "status")
	if !found {
		return map[string]interface{}{"message": "No status available"}, nil
	}

	return status, err
}

// ApplyResult represents the result of applying manifests
type ApplyResult struct {
	AppliedResources []AppliedResource `json:"applied_resources"`
	Errors          []string          `json:"errors,omitempty"`
	Failed          bool              `json:"failed"`
}

// AppliedResource represents a single applied resource
type AppliedResource struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Operation string `json:"operation"` // created, updated
	UID       string `json:"uid"`
}

// ToJSON converts result to JSON
func (r *ApplyResult) ToJSON() string {
	data, _ := json.MarshalIndent(r, "", "  ")
	return string(data)
}

// ServiceStatus represents the status of a Kubernetes Service
type ServiceStatus struct {
	State      string `json:"state"` // healthy, partial, error
	ClusterIP  string `json:"clusterIP,omitempty"`
	ExternalIP string `json:"externalIP,omitempty"`
	NodePort   int32  `json:"nodePort,omitempty"`
	Message    string `json:"message,omitempty"`
}

// GetServiceStatus gets the status of a Kubernetes Service
func (a *ManifestApplier) GetServiceStatus(ctx context.Context, name, namespace string) (*ServiceStatus, error) {
	if namespace == "" {
		namespace = a.namespace
	}

	svc, err := a.clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}

	status := &ServiceStatus{
		State:     "healthy",
		ClusterIP: svc.Spec.ClusterIP,
	}

	// Get NodePort if present
	for _, port := range svc.Spec.Ports {
		if port.NodePort > 0 {
			status.NodePort = port.NodePort
			break
		}
	}

	// Get External IP for LoadBalancer
	if svc.Spec.Type == "LoadBalancer" {
		if len(svc.Status.LoadBalancer.Ingress) > 0 {
			ingress := svc.Status.LoadBalancer.Ingress[0]
			if ingress.IP != "" {
				status.ExternalIP = ingress.IP
			} else if ingress.Hostname != "" {
				status.ExternalIP = ingress.Hostname
			}
		} else {
			status.State = "partial"
			status.Message = "Waiting for LoadBalancer IP assignment"
		}
	}

	return status, nil
}

// DeleteDeployment deletes a Kubernetes Deployment by name and namespace
func (a *ManifestApplier) DeleteDeployment(ctx context.Context, name, namespace string) error {
	if namespace == "" {
		namespace = a.namespace
	}

	a.logger.WithFields(logrus.Fields{
		"kind":      "Deployment",
		"name":      name,
		"namespace": namespace,
	}).Info("Deleting deployment")

	err := a.clientset.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete deployment %s/%s: %w", namespace, name, err)
	}

	return nil
}

// DeleteService deletes a Kubernetes Service by name and namespace
func (a *ManifestApplier) DeleteService(ctx context.Context, name, namespace string) error {
	if namespace == "" {
		namespace = a.namespace
	}

	a.logger.WithFields(logrus.Fields{
		"kind":      "Service",
		"name":      name,
		"namespace": namespace,
	}).Info("Deleting service")

	err := a.clientset.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete service %s/%s: %w", namespace, name, err)
	}

	return nil
}

// DeploymentStatus represents the status of a Kubernetes Deployment
type DeploymentStatus struct {
	State         string `json:"state"` // healthy, partial, error
	Replicas      int32  `json:"replicas"`
	ReadyReplicas int32  `json:"readyReplicas"`
	Message       string `json:"message,omitempty"`
}

// GetDeploymentStatus gets the status of a Kubernetes Deployment
func (a *ManifestApplier) GetDeploymentStatus(ctx context.Context, name, namespace string) (*DeploymentStatus, error) {
	if namespace == "" {
		namespace = a.namespace
	}

	deployment, err := a.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	status := &DeploymentStatus{
		Replicas:      deployment.Status.Replicas,
		ReadyReplicas: deployment.Status.ReadyReplicas,
	}

	// Determine state based on replica counts
	if deployment.Status.ReadyReplicas == 0 && deployment.Status.Replicas > 0 {
		status.State = "error"
		status.Message = "No pods are ready"
	} else if deployment.Status.ReadyReplicas < deployment.Status.Replicas {
		status.State = "partial"
		status.Message = fmt.Sprintf("%d/%d pods ready", deployment.Status.ReadyReplicas, deployment.Status.Replicas)
	} else {
		status.State = "healthy"
		if deployment.Status.Replicas > 0 {
			status.Message = fmt.Sprintf("All %d pods ready", deployment.Status.ReadyReplicas)
		}
	}

	return status, nil
}
// GetClientset returns the Kubernetes clientset
// This is used by service watchers to access the K8s API
func (a *ManifestApplier) GetClientset() *kubernetes.Clientset {
	return a.clientset
}

// GetRestConfig returns the Kubernetes REST config
// This is used by resource watchers to create dynamic clients
func (a *ManifestApplier) GetRestConfig() *rest.Config {
	return a.restConfig
}
