package applier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

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
		return result, fmt.Errorf("failed to apply any resources: %v", result.Errors)
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

// DeleteResource deletes any standard Kubernetes resource by kind, name, and namespace
func (a *ManifestApplier) DeleteResource(ctx context.Context, kind, name, namespace string) error {
	if namespace == "" {
		namespace = a.namespace
	}

	a.logger.WithFields(logrus.Fields{
		"kind":      kind,
		"name":      name,
		"namespace": namespace,
	}).Info("Deleting resource")

	var err error
	switch strings.ToLower(kind) {
	case "deployment":
		err = a.clientset.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	case "service":
		err = a.clientset.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	case "ingress":
		err = a.clientset.NetworkingV1().Ingresses(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	case "configmap":
		err = a.clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	case "persistentvolumeclaim":
		err = a.clientset.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	case "statefulset":
		err = a.clientset.AppsV1().StatefulSets(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	case "cronjob":
		err = a.clientset.BatchV1().CronJobs(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	case "job":
		propagation := metav1.DeletePropagationBackground
		err = a.clientset.BatchV1().Jobs(namespace).Delete(ctx, name, metav1.DeleteOptions{
			PropagationPolicy: &propagation,
		})
	case "daemonset":
		err = a.clientset.AppsV1().DaemonSets(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	default:
		return fmt.Errorf("unsupported resource kind for deletion: %s", kind)
	}

	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete %s %s/%s: %w", kind, namespace, name, err)
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

// IngressStatus represents the status of a Kubernetes Ingress
type IngressStatus struct {
	State                string `json:"state"` // healthy, pending, error
	LoadBalancerIP       string `json:"loadBalancerIP,omitempty"`
	LoadBalancerHostname string `json:"loadBalancerHostname,omitempty"`
	RulesCount           int    `json:"rulesCount,omitempty"`
	Message              string `json:"message,omitempty"`
}

// GetIngressStatus gets the status of a Kubernetes Ingress
func (a *ManifestApplier) GetIngressStatus(ctx context.Context, name, namespace string) (*IngressStatus, error) {
	if namespace == "" {
		namespace = a.namespace
	}

	ingress, err := a.clientset.NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get ingress: %w", err)
	}

	status := &IngressStatus{
		State:      "pending",
		RulesCount: len(ingress.Spec.Rules),
	}

	// Check for LoadBalancer IP/Hostname
	if len(ingress.Status.LoadBalancer.Ingress) > 0 {
		lbIngress := ingress.Status.LoadBalancer.Ingress[0]
		if lbIngress.IP != "" {
			status.LoadBalancerIP = lbIngress.IP
			status.State = "healthy"
			status.Message = "Ingress is active"
		} else if lbIngress.Hostname != "" {
			status.LoadBalancerHostname = lbIngress.Hostname
			status.State = "healthy"
			status.Message = "Ingress is active"
		}
	} else {
		status.Message = "Waiting for LoadBalancer address assignment"
	}

	return status, nil
}


// CheckSecretExists checks if a Kubernetes Secret exists
func (a *ManifestApplier) CheckSecretExists(ctx context.Context, name, namespace string) (bool, error) {
	if namespace == "" {
		namespace = a.namespace
	}

	_, err := a.clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check secret %s/%s: %w", namespace, name, err)
	}

	return true, nil
}

// PVCStatus represents the status of a Kubernetes PersistentVolumeClaim
type PVCStatus struct {
	State      string `json:"state"` // Internal state: Bound, Pending, or error (mapped from Lost phase)
	Capacity   string `json:"capacity,omitempty"`
	VolumeName string `json:"volumeName,omitempty"`
	Message    string `json:"message,omitempty"`
}

// GetPVCStatus gets the status of a Kubernetes PersistentVolumeClaim
func (a *ManifestApplier) GetPVCStatus(ctx context.Context, name, namespace string) (*PVCStatus, error) {
	if namespace == "" {
		namespace = a.namespace
	}

	pvc, err := a.clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get PVC: %w", err)
	}

	status := &PVCStatus{
		State: string(pvc.Status.Phase),
	}

	// Get bound volume name
	if pvc.Spec.VolumeName != "" {
		status.VolumeName = pvc.Spec.VolumeName
	}

	// Get actual capacity
	if storage, ok := pvc.Status.Capacity["storage"]; ok {
		status.Capacity = storage.String()
	}

	// Set message based on state
	switch pvc.Status.Phase {
	case "Bound":
		status.Message = fmt.Sprintf("PVC bound to volume %s", status.VolumeName)
	case "Pending":
		status.Message = "Waiting for volume to be provisioned"
	case "Lost":
		status.State = "error"
		status.Message = "Bound volume has been lost"
	}

	return status, nil
}


// StatefulSetStatus represents the status of a Kubernetes StatefulSet
type StatefulSetStatus struct {
	State           string `json:"state"` // healthy, partial, error
	Replicas        int32  `json:"replicas"`
	ReadyReplicas   int32  `json:"readyReplicas"`
	CurrentReplicas int32  `json:"currentReplicas"`
	Message         string `json:"message,omitempty"`
}

// GetStatefulSetStatus gets the status of a Kubernetes StatefulSet
func (a *ManifestApplier) GetStatefulSetStatus(ctx context.Context, name, namespace string) (*StatefulSetStatus, error) {
	if namespace == "" {
		namespace = a.namespace
	}

	statefulset, err := a.clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	status := &StatefulSetStatus{
		Replicas:        statefulset.Status.Replicas,
		ReadyReplicas:   statefulset.Status.ReadyReplicas,
		CurrentReplicas: statefulset.Status.CurrentReplicas,
	}

	// Determine state based on replica counts
	desiredReplicas := int32(1)
	if statefulset.Spec.Replicas != nil {
		desiredReplicas = *statefulset.Spec.Replicas
	}

	if status.ReadyReplicas == desiredReplicas && desiredReplicas > 0 {
		status.State = "healthy"
		status.Message = fmt.Sprintf("All %d replicas are ready", desiredReplicas)
	} else if status.ReadyReplicas > 0 {
		status.State = "partial"
		status.Message = fmt.Sprintf("%d of %d replicas ready", status.ReadyReplicas, desiredReplicas)
	} else if desiredReplicas == 0 {
		status.State = "healthy"
		status.Message = "StatefulSet scaled to 0"
	} else {
		status.State = "error"
		status.Message = "No replicas are ready"
	}

	return status, nil
}


// DeleteCRD deletes a Custom Resource Definition object by group, version, kind, name and namespace
func (a *ManifestApplier) DeleteCRD(ctx context.Context, group, version, kind, name, namespace string) error {
	if namespace == "" {
		namespace = a.namespace
	}

	a.logger.WithFields(logrus.Fields{
		"group":     group,
		"version":   version,
		"kind":      kind,
		"name":      name,
		"namespace": namespace,
	}).Info("Deleting CRD resource")

	// Build the GVR (GroupVersionResource) for the CRD
	gvr := schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: pluralize(kind), // CRDs typically use pluralized resource names
	}

	// Try to delete using the dynamic client
	err := a.dynamicClient.Resource(gvr).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		// If the pluralized name doesn't work, try the singular lowercase kind as a fallback
		gvr.Resource = strings.ToLower(kind)
		err = a.dynamicClient.Resource(gvr).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete CRD %s/%s/%s %s/%s: %w", group, version, kind, namespace, name, err)
		}
	}

	return nil
}

// pluralize returns a simple pluralized form of a word
func pluralize(word string) string {
	if word == "" {
		return word
	}
	lower := toLowerPlural(word)
	return lower
}

// toLowerPlural converts a kind to its lowercase plural form
func toLowerPlural(kind string) string {
	lower := ""
	for i, r := range kind {
		if i > 0 && r >= 'A' && r <= 'Z' {
			lower += string(r + 32) // Convert to lowercase
		} else if r >= 'A' && r <= 'Z' {
			lower += string(r + 32)
		} else {
			lower += string(r)
		}
	}
	// Simple pluralization rules
	if len(lower) > 0 {
		lastChar := lower[len(lower)-1]
		if lastChar == 's' || lastChar == 'x' || lastChar == 'z' {
			return lower + "es"
		}
		if lastChar == 'y' && len(lower) > 1 {
			prevChar := lower[len(lower)-2]
			if prevChar != 'a' && prevChar != 'e' && prevChar != 'i' && prevChar != 'o' && prevChar != 'u' {
				return lower[:len(lower)-1] + "ies"
			}
		}
		return lower + "s"
	}
	return lower
}
