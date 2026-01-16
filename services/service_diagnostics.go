package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// DiagnosticCheck represents a single diagnostic check result
type DiagnosticCheck struct {
	Name        string `json:"name"`
	Status      string `json:"status"`      // "pass", "warning", "error"
	Message     string `json:"message"`
	Suggestion  string `json:"suggestion"`
	AutoFixable bool   `json:"autoFixable"`
	FixType     string `json:"fixType,omitempty"` // "metallb-pool", "selector-fix", etc.
}

// ServiceDiagnosticResult contains all diagnostic results for a service
type ServiceDiagnosticResult struct {
	OverallStatus string            `json:"overallStatus"` // "healthy", "warning", "error"
	ServiceName   string            `json:"serviceName"`
	Namespace     string            `json:"namespace"`
	Checks        []DiagnosticCheck `json:"checks"`
	Timestamp     time.Time         `json:"timestamp"`
}

// ServiceDiagnostics handles service health diagnostics
type ServiceDiagnostics struct {
	clientset *kubernetes.Clientset
	logger    *logrus.Logger
}

// NewServiceDiagnostics creates a new ServiceDiagnostics instance
func NewServiceDiagnostics(clientset *kubernetes.Clientset) *ServiceDiagnostics {
	return &ServiceDiagnostics{
		clientset: clientset,
		logger:    logrus.New(),
	}
}

// DiagnoseService runs all diagnostic checks on a service
func (sd *ServiceDiagnostics) DiagnoseService(ctx context.Context, namespace, name string) (*ServiceDiagnosticResult, error) {
	result := &ServiceDiagnosticResult{
		OverallStatus: "healthy",
		ServiceName:   name,
		Namespace:     namespace,
		Checks:        []DiagnosticCheck{},
		Timestamp:     time.Now(),
	}

	// Get the service
	svc, err := sd.clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		result.OverallStatus = "error"
		result.Checks = append(result.Checks, DiagnosticCheck{
			Name:        "service-exists",
			Status:      "error",
			Message:     fmt.Sprintf("Service not found: %s", err.Error()),
			Suggestion:  "Deploy the service through the workflow",
			AutoFixable: false,
		})
		return result, nil
	}

	// Run all diagnostic checks
	checks := []DiagnosticCheck{}

	// Check 1: Service exists (passed if we got here)
	checks = append(checks, DiagnosticCheck{
		Name:        "service-exists",
		Status:      "pass",
		Message:     "Service exists in cluster",
		AutoFixable: false,
	})

	// Check 2: Endpoints populated
	endpointCheck := sd.checkEndpoints(ctx, namespace, name)
	checks = append(checks, endpointCheck)

	// Check 3: LoadBalancer provisioning (if applicable)
	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		lbCheck := sd.checkLoadBalancer(ctx, svc)
		checks = append(checks, lbCheck)
	}

	// Check 4: Selector matches pods
	selectorCheck := sd.checkSelectorMatch(ctx, namespace, svc.Spec.Selector)
	checks = append(checks, selectorCheck)

	// Check 5: Target pods ready
	podsCheck := sd.checkTargetPodsReady(ctx, namespace, svc.Spec.Selector)
	checks = append(checks, podsCheck)

	// Check 6: Port configuration
	portCheck := sd.checkPortConfiguration(ctx, namespace, svc)
	checks = append(checks, portCheck)

	result.Checks = checks

	// Determine overall status
	for _, check := range checks {
		if check.Status == "error" {
			result.OverallStatus = "error"
			break
		} else if check.Status == "warning" && result.OverallStatus != "error" {
			result.OverallStatus = "warning"
		}
	}

	return result, nil
}

// checkEndpoints checks if the service has endpoints
func (sd *ServiceDiagnostics) checkEndpoints(ctx context.Context, namespace, name string) DiagnosticCheck {
	endpoints, err := sd.clientset.CoreV1().Endpoints(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return DiagnosticCheck{
			Name:        "endpoints-exist",
			Status:      "error",
			Message:     "Unable to fetch endpoints",
			Suggestion:  "Check if target pods exist and are ready",
			AutoFixable: false,
		}
	}

	totalAddresses := 0
	for _, subset := range endpoints.Subsets {
		totalAddresses += len(subset.Addresses)
	}

	if totalAddresses == 0 {
		return DiagnosticCheck{
			Name:        "endpoints-exist",
			Status:      "error",
			Message:     "No endpoints available - no ready pods match the service selector",
			Suggestion:  "Check if target pods are running and ready, or verify the selector matches pod labels",
			AutoFixable: false,
		}
	}

	return DiagnosticCheck{
		Name:        "endpoints-exist",
		Status:      "pass",
		Message:     fmt.Sprintf("%d endpoint(s) available", totalAddresses),
		AutoFixable: false,
	}
}

// checkLoadBalancer checks LoadBalancer provisioning status
func (sd *ServiceDiagnostics) checkLoadBalancer(ctx context.Context, svc *corev1.Service) DiagnosticCheck {
	if len(svc.Status.LoadBalancer.Ingress) > 0 {
		ingress := svc.Status.LoadBalancer.Ingress[0]
		ip := ingress.IP
		if ip == "" {
			ip = ingress.Hostname
		}
		return DiagnosticCheck{
			Name:        "loadbalancer-provisioned",
			Status:      "pass",
			Message:     fmt.Sprintf("LoadBalancer provisioned with External IP: %s", ip),
			AutoFixable: false,
		}
	}

	// Check if MetalLB is installed
	metallbInstalled := sd.isMetalLBInstalled(ctx)

	if !metallbInstalled {
		return DiagnosticCheck{
			Name:        "loadbalancer-provisioned",
			Status:      "error",
			Message:     "LoadBalancer External-IP pending - MetalLB not installed",
			Suggestion:  "Install MetalLB or use a cloud provider with LoadBalancer support",
			AutoFixable: false,
		}
	}

	// MetalLB is installed but no IP assigned - likely IP pool issue
	return DiagnosticCheck{
		Name:        "loadbalancer-provisioned",
		Status:      "error",
		Message:     "LoadBalancer External-IP pending - MetalLB IP pool may not be configured",
		Suggestion:  "Configure MetalLB IP address pool with available IPs",
		AutoFixable: true,
		FixType:     "metallb-pool",
	}
}

// isMetalLBInstalled checks if MetalLB is installed in the cluster
func (sd *ServiceDiagnostics) isMetalLBInstalled(ctx context.Context) bool {
	_, err := sd.clientset.CoreV1().Namespaces().Get(ctx, "metallb-system", metav1.GetOptions{})
	return err == nil
}

// checkSelectorMatch checks if the service selector matches any pods
func (sd *ServiceDiagnostics) checkSelectorMatch(ctx context.Context, namespace string, selector map[string]string) DiagnosticCheck {
	if len(selector) == 0 {
		return DiagnosticCheck{
			Name:        "selector-match",
			Status:      "warning",
			Message:     "Service has no selector defined",
			Suggestion:  "Add a selector to route traffic to specific pods",
			AutoFixable: true,
			FixType:     "selector-fix",
		}
	}

	labelSelector := labels.SelectorFromSet(selector)
	pods, err := sd.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})

	if err != nil {
		return DiagnosticCheck{
			Name:        "selector-match",
			Status:      "warning",
			Message:     "Unable to check selector match",
			Suggestion:  "Check cluster connectivity",
			AutoFixable: false,
		}
	}

	if len(pods.Items) == 0 {
		return DiagnosticCheck{
			Name:        "selector-match",
			Status:      "error",
			Message:     fmt.Sprintf("No pods match selector: %v", selector),
			Suggestion:  "Update the selector to match your deployment's pod labels, or check if the deployment exists",
			AutoFixable: true,
			FixType:     "selector-fix",
		}
	}

	return DiagnosticCheck{
		Name:        "selector-match",
		Status:      "pass",
		Message:     fmt.Sprintf("Selector matches %d pod(s)", len(pods.Items)),
		AutoFixable: false,
	}
}

// checkTargetPodsReady checks if target pods are in ready state
func (sd *ServiceDiagnostics) checkTargetPodsReady(ctx context.Context, namespace string, selector map[string]string) DiagnosticCheck {
	if len(selector) == 0 {
		return DiagnosticCheck{
			Name:        "pods-ready",
			Status:      "warning",
			Message:     "Cannot check pods - no selector defined",
			AutoFixable: false,
		}
	}

	labelSelector := labels.SelectorFromSet(selector)
	pods, err := sd.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})

	if err != nil || len(pods.Items) == 0 {
		return DiagnosticCheck{
			Name:        "pods-ready",
			Status:      "warning",
			Message:     "No pods found to check",
			AutoFixable: false,
		}
	}

	readyCount := 0
	notReadyPods := []string{}

	for _, pod := range pods.Items {
		isReady := false
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				isReady = true
				break
			}
		}
		if isReady {
			readyCount++
		} else {
			// Get the reason for not ready
			reason := string(pod.Status.Phase)
			for _, containerStatus := range pod.Status.ContainerStatuses {
				if containerStatus.State.Waiting != nil {
					reason = containerStatus.State.Waiting.Reason
					break
				}
			}
			notReadyPods = append(notReadyPods, fmt.Sprintf("%s (%s)", pod.Name, reason))
		}
	}

	if readyCount == 0 {
		return DiagnosticCheck{
			Name:        "pods-ready",
			Status:      "error",
			Message:     fmt.Sprintf("No pods are ready: %s", strings.Join(notReadyPods, ", ")),
			Suggestion:  "Check pod logs and events for errors",
			AutoFixable: false,
		}
	}

	if readyCount < len(pods.Items) {
		return DiagnosticCheck{
			Name:        "pods-ready",
			Status:      "warning",
			Message:     fmt.Sprintf("%d/%d pods ready. Not ready: %s", readyCount, len(pods.Items), strings.Join(notReadyPods, ", ")),
			Suggestion:  "Some pods are not ready - check pod logs",
			AutoFixable: false,
		}
	}

	return DiagnosticCheck{
		Name:        "pods-ready",
		Status:      "pass",
		Message:     fmt.Sprintf("All %d pod(s) are ready", readyCount),
		AutoFixable: false,
	}
}

// checkPortConfiguration checks if service ports match container ports
func (sd *ServiceDiagnostics) checkPortConfiguration(ctx context.Context, namespace string, svc *corev1.Service) DiagnosticCheck {
	if len(svc.Spec.Selector) == 0 || len(svc.Spec.Ports) == 0 {
		return DiagnosticCheck{
			Name:        "port-config",
			Status:      "warning",
			Message:     "Cannot verify port configuration - no selector or ports defined",
			AutoFixable: false,
		}
	}

	labelSelector := labels.SelectorFromSet(svc.Spec.Selector)
	pods, err := sd.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})

	if err != nil || len(pods.Items) == 0 {
		return DiagnosticCheck{
			Name:        "port-config",
			Status:      "warning",
			Message:     "Cannot verify port configuration - no matching pods found",
			AutoFixable: false,
		}
	}

	// Get container ports from first pod
	containerPorts := make(map[int32]bool)
	for _, container := range pods.Items[0].Spec.Containers {
		for _, port := range container.Ports {
			containerPorts[port.ContainerPort] = true
		}
	}

	// Check if service target ports exist in containers
	missingPorts := []int32{}
	for _, svcPort := range svc.Spec.Ports {
		targetPort := svcPort.TargetPort.IntVal
		if targetPort == 0 {
			targetPort = svcPort.Port
		}
		if !containerPorts[targetPort] {
			missingPorts = append(missingPorts, targetPort)
		}
	}

	if len(missingPorts) > 0 {
		return DiagnosticCheck{
			Name:        "port-config",
			Status:      "warning",
			Message:     fmt.Sprintf("Target port(s) %v not exposed by container", missingPorts),
			Suggestion:  "Verify targetPort matches a containerPort in your deployment",
			AutoFixable: true,
			FixType:     "port-fix",
		}
	}

	return DiagnosticCheck{
		Name:        "port-config",
		Status:      "pass",
		Message:     "Port configuration is valid",
		AutoFixable: false,
	}
}
