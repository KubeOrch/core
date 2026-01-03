package services

import (
	"bytes"
	"context"
	"fmt"
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

// getMetalLBPoolTemplate generates MetalLB IP pool configuration
func (fts *FixTemplateService) getMetalLBPoolTemplate(ctx context.Context, params map[string]interface{}) (*FixTemplate, error) {
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

	tmpl := `apiVersion: v1
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
      - {{.IPRange}}`

	t, err := template.New("metallb").Parse(tmpl)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, map[string]string{"IPRange": ipRange})
	if err != nil {
		return nil, err
	}

	return &FixTemplate{
		Name:        "Configure MetalLB IP Pool",
		Description: fmt.Sprintf("Creates an IP address pool (%s) for MetalLB to assign LoadBalancer IPs", ipRange),
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
			parts := splitIP(ip)
			if len(parts) == 4 {
				return fmt.Sprintf("%s.%s.%s.100-%s.%s.%s.110", parts[0], parts[1], parts[2], parts[0], parts[1], parts[2]), nil
			}
		}
	}

	return "", fmt.Errorf("cannot detect IP range from node IPs")
}

// splitIP splits an IP address into parts
func splitIP(ip string) []string {
	result := []string{}
	current := ""
	for _, c := range ip {
		if c == '.' {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	result = append(result, current)
	return result
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
