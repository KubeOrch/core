package kubernetes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/homedir"
	"k8s.io/client-go/util/retry"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

type KubeConfigLoader struct {
	kubeconfigPath string
	context        string
	namespace      string
}

func NewKubeConfigLoader() *KubeConfigLoader {
	return &KubeConfigLoader{
		namespace: "default",
	}
}

func (k *KubeConfigLoader) WithKubeConfigPath(path string) *KubeConfigLoader {
	k.kubeconfigPath = path
	return k
}

func (k *KubeConfigLoader) WithContext(context string) *KubeConfigLoader {
	k.context = context
	return k
}

func (k *KubeConfigLoader) WithNamespace(namespace string) *KubeConfigLoader {
	k.namespace = namespace
	return k
}

func (k *KubeConfigLoader) Load() (*rest.Config, error) {
	var config *rest.Config
	var err error

	if k.kubeconfigPath != "" {
		config, err = k.loadFromFile(k.kubeconfigPath)
	} else {
		config, err = k.loadAutoDetect()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to load kubernetes config: %w", err)
	}

	k.applyRateLimiting(config)
	return config, nil
}

func (k *KubeConfigLoader) loadFromFile(path string) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.ExplicitPath = path

	configOverrides := &clientcmd.ConfigOverrides{}
	if k.context != "" {
		configOverrides.CurrentContext = k.context
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		configOverrides,
	)

	return clientConfig.ClientConfig()
}

func (k *KubeConfigLoader) loadAutoDetect() (*rest.Config, error) {
	if config, err := rest.InClusterConfig(); err == nil {
		return config, nil
	}

	kubeconfigPath := k.getDefaultKubeConfigPath()
	if kubeconfigPath == "" {
		return nil, fmt.Errorf("unable to find kubeconfig: not in cluster and no kubeconfig file found")
	}

	return k.loadFromFile(kubeconfigPath)
}

func (k *KubeConfigLoader) getDefaultKubeConfigPath() string {
	if envPath := os.Getenv("KUBECONFIG"); envPath != "" {
		return envPath
	}

	if home := homedir.HomeDir(); home != "" {
		path := filepath.Join(home, ".kube", "config")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

func (k *KubeConfigLoader) applyRateLimiting(config *rest.Config) {
	config.QPS = 100
	config.Burst = 100
}

func (k *KubeConfigLoader) LoadRawConfig() (*api.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()

	if k.kubeconfigPath != "" {
		loadingRules.ExplicitPath = k.kubeconfigPath
	} else if envPath := os.Getenv("KUBECONFIG"); envPath != "" {
		loadingRules.ExplicitPath = envPath
	}

	return loadingRules.Load()
}

func ValidateClusterConnection(ctx context.Context, config *rest.Config) error {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	_, err = clientset.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("failed to connect to kubernetes cluster: %w", err)
	}

	_, err = clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		if errors.IsForbidden(err) {
			return fmt.Errorf("insufficient permissions: %w", err)
		}
		return fmt.Errorf("failed to validate cluster access: %w", err)
	}

	return nil
}

func ValidateClusterConnectionWithRetry(ctx context.Context, config *rest.Config) error {
	return retry.OnError(retry.DefaultRetry, func(err error) bool {
		return !errors.IsUnauthorized(err) && !errors.IsForbidden(err)
	}, func() error {
		return ValidateClusterConnection(ctx, config)
	})
}

type ClusterInfo struct {
	Name      string
	Server    string
	AuthInfo  string
	Namespace string
	Current   bool
}

func ListAvailableClusters(kubeconfigPath string) ([]ClusterInfo, error) {
	loader := NewKubeConfigLoader().WithKubeConfigPath(kubeconfigPath)
	rawConfig, err := loader.LoadRawConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	var clusters []ClusterInfo
	for name, context := range rawConfig.Contexts {
		clusterConfig, exists := rawConfig.Clusters[context.Cluster]
		if !exists {
			continue
		}

		namespace := context.Namespace
		if namespace == "" {
			namespace = "default"
		}

		clusters = append(clusters, ClusterInfo{
			Name:      name,
			Server:    clusterConfig.Server,
			AuthInfo:  context.AuthInfo,
			Namespace: namespace,
			Current:   name == rawConfig.CurrentContext,
		})
	}

	return clusters, nil
}

func GetConfigForContext(kubeconfigPath, contextName string) (*rest.Config, error) {
	loader := NewKubeConfigLoader().
		WithKubeConfigPath(kubeconfigPath).
		WithContext(contextName)

	return loader.Load()
}

func IsInCluster() bool {
	_, err := rest.InClusterConfig()
	return err == nil
}
