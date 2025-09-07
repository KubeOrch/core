package kubernetes

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

// CreateClientFromConfig creates a Kubernetes clientset from a REST config
func CreateClientFromConfig(config *rest.Config) (*kubernetes.Clientset, error) {
	// Apply default rate limiting
	config.QPS = 100
	config.Burst = 100
	
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	
	return clientset, nil
}

// CreateClientFromAuth creates a Kubernetes clientset from auth config
func CreateClientFromAuth(auth *AuthConfig) (*kubernetes.Clientset, error) {
	config, err := auth.BuildRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build REST config: %w", err)
	}
	
	return CreateClientFromConfig(config)
}