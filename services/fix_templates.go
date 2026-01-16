package services

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// FixTemplate represents a fix template with YAML content
type FixTemplate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	YAML        string `json:"yaml"`
	ApplyMethod string `json:"applyMethod"` // "kubectl-apply", "direct-api"
	FixType     string `json:"fixType"`
}

// EnvironmentInfo contains detected cluster environment information
type EnvironmentInfo struct {
	IsCloudProvider bool   // Has cloud provider (AWS, GCP, Azure, Alibaba, etc.)
	Provider        string // Provider name if detected
	IsMinikube      bool   // Running on minikube
	IsBareMetal     bool   // Self-hosted bare metal
	HasMetalLB      bool   // MetalLB is installed
}

// FixTemplateService generates fix templates
type FixTemplateService struct {
	clientset *kubernetes.Clientset
	logger    *logrus.Logger
}

// NewFixTemplateService creates a new FixTemplateService
func NewFixTemplateService(clientset *kubernetes.Clientset) *FixTemplateService {
	return &FixTemplateService{
		clientset: clientset,
		logger:    logrus.New(),
	}
}

// GetFixTemplate generates a fix template based on fix type and parameters
func (fts *FixTemplateService) GetFixTemplate(ctx context.Context, fixType string, params map[string]interface{}) (*FixTemplate, error) {
	switch fixType {
	case "metallb-pool":
		return fts.getMetalLBPoolTemplate(ctx, params)
	case "selector-fix":
		return fts.getSelectorFixTemplate(params)
	case "port-fix":
		return fts.getPortFixTemplate(params)
	default:
		return nil, fmt.Errorf("unknown fix type: %s", fixType)
	}
}

// detectEnvironment detects the cluster environment type
func (fts *FixTemplateService) detectEnvironment(ctx context.Context) (*EnvironmentInfo, error) {
	envInfo := &EnvironmentInfo{}

	// Get nodes to check providerID and labels
	nodes, err := fts.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil || len(nodes.Items) == 0 {
		return envInfo, fmt.Errorf("failed to get nodes: %w", err)
	}

	node := nodes.Items[0]

	// Check providerID - if exists and not empty, it's a cloud provider
	// Examples: "aws://us-east-1a/i-abc123", "gce://project/zone/instance", "azure://...", "alicloud://..."
	if node.Spec.ProviderID != "" {
		envInfo.IsCloudProvider = true

		// Extract provider name from providerID (format: "provider://...")
		if idx := strings.Index(node.Spec.ProviderID, "://"); idx > 0 {
			envInfo.Provider = node.Spec.ProviderID[:idx]
		} else {
			envInfo.Provider = "unknown cloud provider"
		}

		fts.logger.WithFields(logrus.Fields{
			"providerID": node.Spec.ProviderID,
			"provider":   envInfo.Provider,
		}).Info("Detected cloud provider cluster")

		return envInfo, nil
	}

	// No providerID - it's self-hosted (bare metal or local dev)
	// Check node labels to distinguish minikube/k3s/kind from bare metal
	labels := node.Labels

	if _, hasMinikube := labels["minikube.k8s.io/name"]; hasMinikube {
		envInfo.IsMinikube = true
		fts.logger.Info("Detected minikube cluster")
	} else if _, hasK3s := labels["k3s.io/hostname"]; hasK3s {
		envInfo.IsBareMetal = true // k3s is often used for prod
		fts.logger.Info("Detected k3s cluster")
	} else if _, hasKind := labels["kind.x-k8s.io/cluster"]; hasKind {
		envInfo.IsMinikube = true // Treat kind like minikube (local dev)
		fts.logger.Info("Detected kind cluster")
	} else {
		envInfo.IsBareMetal = true
		fts.logger.Info("Detected bare metal cluster")
	}

	// Check if MetalLB is installed (look for metallb-system namespace)
	_, err = fts.clientset.CoreV1().Namespaces().Get(ctx, "metallb-system", metav1.GetOptions{})
	if err == nil {
		envInfo.HasMetalLB = true
		fts.logger.Info("MetalLB installation detected")
	} else {
		fts.logger.Info("MetalLB not found in cluster")
	}

	return envInfo, nil
}

// getMetalLBPoolTemplate generates MetalLB IP pool configuration
// This includes both IP pool AND strictARP fix for comprehensive setup
func (fts *FixTemplateService) getMetalLBPoolTemplate(ctx context.Context, params map[string]interface{}) (*FixTemplate, error) {
	// Detect environment
	envInfo, err := fts.detectEnvironment(ctx)
	if err != nil {
		fts.logger.WithError(err).Warn("Failed to detect environment, proceeding with default")
		// Use default env info if detection fails
		envInfo = &EnvironmentInfo{
			HasMetalLB:  true, // Assume MetalLB exists if we're trying to fix it
			IsBareMetal: true,
		}
	}

	// If cloud provider detected, LoadBalancer should work - no MetalLB needed
	if envInfo.IsCloudProvider {
		return nil, fmt.Errorf("cluster appears to be running on a cloud provider (%s) - LoadBalancer services should work without MetalLB", envInfo.Provider)
	}

	// Check if MetalLB is installed
	if !envInfo.HasMetalLB {
		return nil, fmt.Errorf("MetalLB is not installed - please install MetalLB first for LoadBalancer support on self-hosted clusters")
	}

	// Try to detect appropriate IP range
	ipRange, err := fts.detectIPRange(ctx)
	if err != nil {
		// Use provided IP range or default
		if providedRange, ok := params["ipRange"].(string); ok {
			ipRange = providedRange
		} else {
			ipRange = "192.168.49.100-192.168.49.110"
		}
	}

	// Generate comprehensive fix: IP pool + strictARP
	tmpl := `---
# MetalLB IP Address Pool
apiVersion: v1
kind: ConfigMap
metadata:
  namespace: metallb-system
  name: config
data:
  config: |
    address-pools:
    - name: default
      protocol: layer2
      addresses:
      - {{.IPRange}}
---
# kube-proxy strictARP Configuration (Required for MetalLB Layer 2)
apiVersion: v1
kind: ConfigMap
metadata:
  name: kube-proxy
  namespace: kube-system
data:
  config.conf: |
    apiVersion: kubeproxy.config.k8s.io/v1alpha1
    kind: KubeProxyConfiguration
    mode: "iptables"
    ipvs:
      strictARP: true`

	t, err := template.New("metallb").Parse(tmpl)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, map[string]string{"IPRange": ipRange})
	if err != nil {
		return nil, err
	}

	// Build simple description
	description := fmt.Sprintf("Configures MetalLB IP pool (%s) and strictARP", ipRange)

	return &FixTemplate{
		Name:        "Configure MetalLB",
		Description: description,
		YAML:        buf.String(),
		ApplyMethod: "kubectl-apply",
		FixType:     "metallb-pool",
	}, nil
}

// detectIPRange tries to detect an appropriate IP range for MetalLB
func (fts *FixTemplateService) detectIPRange(ctx context.Context) (string, error) {
	// Try to get nodes to determine network
	nodes, err := fts.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil || len(nodes.Items) == 0 {
		return "", fmt.Errorf("cannot detect IP range")
	}

	// Get internal IP of first node
	for _, addr := range nodes.Items[0].Status.Addresses {
		if addr.Type == "InternalIP" {
			// Parse IP and suggest range in same subnet
			ip := addr.Address
			// Simple heuristic: use .100-.110 in same /24 subnet
			parts := strings.Split(ip, ".")
			if len(parts) == 4 {
				return fmt.Sprintf("%s.%s.%s.100-%s.%s.%s.110", parts[0], parts[1], parts[2], parts[0], parts[1], parts[2]), nil
			}
		}
	}

	return "", fmt.Errorf("cannot detect IP range from node IPs")
}

// getSelectorFixTemplate generates a service selector fix
func (fts *FixTemplateService) getSelectorFixTemplate(params map[string]interface{}) (*FixTemplate, error) {
	serviceName := getStringParam(params, "serviceName", "my-service")
	namespace := getStringParam(params, "namespace", "default")
	targetApp := getStringParam(params, "targetApp", "my-app")

	tmpl := `apiVersion: v1
kind: Service
metadata:
  name: {{.ServiceName}}
  namespace: {{.Namespace}}
spec:
  selector:
    app: {{.TargetApp}}`

	t, err := template.New("selector").Parse(tmpl)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, map[string]string{
		"ServiceName": serviceName,
		"Namespace":   namespace,
		"TargetApp":   targetApp,
	})
	if err != nil {
		return nil, err
	}

	return &FixTemplate{
		Name:        "Fix Service Selector",
		Description: fmt.Sprintf("Updates the service selector to target app=%s", targetApp),
		YAML:        buf.String(),
		ApplyMethod: "kubectl-apply",
		FixType:     "selector-fix",
	}, nil
}

// getPortFixTemplate generates a port configuration fix
func (fts *FixTemplateService) getPortFixTemplate(params map[string]interface{}) (*FixTemplate, error) {
	serviceName := getStringParam(params, "serviceName", "my-service")
	namespace := getStringParam(params, "namespace", "default")
	servicePort := getIntParam(params, "servicePort", 80)
	targetPort := getIntParam(params, "targetPort", 8080)
	protocol := getStringParam(params, "protocol", "TCP")

	tmpl := `apiVersion: v1
kind: Service
metadata:
  name: {{.ServiceName}}
  namespace: {{.Namespace}}
spec:
  ports:
  - port: {{.ServicePort}}
    targetPort: {{.TargetPort}}
    protocol: {{.Protocol}}
    name: http`

	t, err := template.New("port").Parse(tmpl)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, map[string]interface{}{
		"ServiceName": serviceName,
		"Namespace":   namespace,
		"ServicePort": servicePort,
		"TargetPort":  targetPort,
		"Protocol":    protocol,
	})
	if err != nil {
		return nil, err
	}

	return &FixTemplate{
		Name:        "Fix Port Configuration",
		Description: fmt.Sprintf("Updates service ports: port=%d, targetPort=%d", servicePort, targetPort),
		YAML:        buf.String(),
		ApplyMethod: "kubectl-apply",
		FixType:     "port-fix",
	}, nil
}

// Helper functions to get params with defaults
func getStringParam(params map[string]interface{}, key, defaultVal string) string {
	if val, ok := params[key].(string); ok {
		return val
	}
	return defaultVal
}

func getIntParam(params map[string]interface{}, key string, defaultVal int) int {
	if val, ok := params[key].(float64); ok {
		return int(val)
	}
	if val, ok := params[key].(int); ok {
		return val
	}
	return defaultVal
}

// ApplyFix applies a fix using the manifest applier
func (fts *FixTemplateService) ApplyFix(ctx context.Context, yaml string, applier interface{}) error {
	// Use the manifest applier to apply the YAML
	// This will be called from the handler
	return nil
}
