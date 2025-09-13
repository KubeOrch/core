package kubernetes

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/KubeOrch/core/models"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd/api"
)

// Package-level logger initialized once
var logger = logrus.WithField("package", "kubernetes")

type AuthConfig struct {
	Type models.ClusterAuthType

	KubeConfigPath    string
	KubeConfigContent string

	ServiceAccountToken string
	ServiceAccountPath  string

	BearerToken     string
	BearerTokenFile string

	ClientCertData string
	ClientKeyData  string
	ClientCertPath string
	ClientKeyPath  string
	CAData         string
	CAPath         string

	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRefreshToken string
	OIDCScopes       []string

	ExecCommand string
	ExecArgs    []string
	ExecEnv     map[string]string

	ServerURL string
	Insecure  bool
}

func NewAuthConfig(authType models.ClusterAuthType) *AuthConfig {
	return &AuthConfig{
		Type: authType,
	}
}

func (a *AuthConfig) BuildRESTConfig() (*rest.Config, error) {
	logger.WithFields(logrus.Fields{
		"auth_type": a.Type,
		"server":    a.ServerURL,
	}).Info("Building REST config for auth type")
	
	switch a.Type {
	case models.ClusterAuthKubeConfig:
		return a.buildKubeConfigAuth()
	case models.ClusterAuthServiceAccount:
		return a.buildServiceAccountAuth()
	case models.ClusterAuthToken:
		return a.buildTokenAuth()
	case models.ClusterAuthCertificate:
		return a.buildCertificateAuth()
	case models.ClusterAuthOIDC:
		return a.buildOIDCAuth()
	default:
		return nil, fmt.Errorf("unsupported auth type: %s", a.Type)
	}
}

func (a *AuthConfig) buildKubeConfigAuth() (*rest.Config, error) {
	if a.KubeConfigContent != "" {
		tempFile, err := os.CreateTemp("", "kubeconfig-*.yaml")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp kubeconfig file: %w", err)
		}
		defer tempFile.Close()

		if _, err := tempFile.WriteString(a.KubeConfigContent); err != nil {
			return nil, fmt.Errorf("failed to write kubeconfig content: %w", err)
		}

		a.KubeConfigPath = tempFile.Name()
	}

	loader := NewKubeConfigLoader().WithKubeConfigPath(a.KubeConfigPath)
	return loader.Load()
}

func (a *AuthConfig) buildServiceAccountAuth() (*rest.Config, error) {
	config := &rest.Config{
		Host: a.ServerURL,
		TLSClientConfig: rest.TLSClientConfig{},
	}

	if a.ServiceAccountToken != "" {
		config.BearerToken = a.ServiceAccountToken
	} else if a.ServiceAccountPath != "" {
		config.BearerTokenFile = a.ServiceAccountPath
	} else {
		return rest.InClusterConfig()
	}

	if err := a.applyTLSConfig(config); err != nil {
		return nil, err
	}

	return config, nil
}

func (a *AuthConfig) buildTokenAuth() (*rest.Config, error) {
	if a.ServerURL == "" {
		return nil, fmt.Errorf("server URL is required")
	}
	
	config := &rest.Config{
		Host: a.ServerURL,
		TLSClientConfig: rest.TLSClientConfig{},
	}

	if a.BearerToken != "" {
		config.BearerToken = a.BearerToken
		tokenPreview := a.BearerToken
		if len(tokenPreview) > 20 {
			tokenPreview = tokenPreview[:20] + "..."
		}
		logger.WithFields(logrus.Fields{
			"server":          a.ServerURL,
			"token_length":    len(a.BearerToken),
			"token_preview":   tokenPreview,
		}).Info("Using bearer token authentication")
	} else if a.BearerTokenFile != "" {
		config.BearerTokenFile = a.BearerTokenFile
		logger.WithFields(logrus.Fields{
			"server":     a.ServerURL,
			"token_file": a.BearerTokenFile,
		}).Info("Using bearer token file authentication")
	} else {
		logger.Error("No bearer token or token file provided")
		return nil, fmt.Errorf("bearer token or token file must be provided")
	}

	// Apply TLS configuration
	if err := a.applyTLSConfig(config); err != nil {
		logger.WithError(err).Error("Failed to apply TLS config")
		return nil, err
	}

	logger.WithFields(logrus.Fields{
		"server":       a.ServerURL,
		"insecure":     config.TLSClientConfig.Insecure,
		"has_ca":       len(config.TLSClientConfig.CAData) > 0,
		"has_ca_file":  config.TLSClientConfig.CAFile != "",
	}).Info("Token auth config built successfully")

	return config, nil
}

func (a *AuthConfig) buildCertificateAuth() (*rest.Config, error) {
	config := &rest.Config{
		Host: a.ServerURL,
		TLSClientConfig: rest.TLSClientConfig{},
	}

	if a.ClientCertData != "" && a.ClientKeyData != "" {
		certData, err := base64.StdEncoding.DecodeString(a.ClientCertData)
		if err != nil {
			return nil, fmt.Errorf("failed to decode client cert data: %w", err)
		}
		keyData, err := base64.StdEncoding.DecodeString(a.ClientKeyData)
		if err != nil {
			return nil, fmt.Errorf("failed to decode client key data: %w", err)
		}
		config.TLSClientConfig.CertData = certData
		config.TLSClientConfig.KeyData = keyData
	} else if a.ClientCertPath != "" && a.ClientKeyPath != "" {
		config.TLSClientConfig.CertFile = a.ClientCertPath
		config.TLSClientConfig.KeyFile = a.ClientKeyPath
	} else {
		return nil, fmt.Errorf("client certificate and key must be provided")
	}

	if err := a.applyTLSConfig(config); err != nil {
		return nil, err
	}

	return config, nil
}

func (a *AuthConfig) buildOIDCAuth() (*rest.Config, error) {
	config := &rest.Config{
		Host: a.ServerURL,
		TLSClientConfig: rest.TLSClientConfig{},
		AuthProvider: &api.AuthProviderConfig{
			Name: "oidc",
			Config: map[string]string{
				"idp-issuer-url": a.OIDCIssuerURL,
				"client-id":      a.OIDCClientID,
				"client-secret":  a.OIDCClientSecret,
				"refresh-token":  a.OIDCRefreshToken,
			},
		},
	}

	if len(a.OIDCScopes) > 0 {
		scopesStr := ""
		for i, scope := range a.OIDCScopes {
			if i > 0 {
				scopesStr += ","
			}
			scopesStr += scope
		}
		config.AuthProvider.Config["extra-scopes"] = scopesStr
	}

	if err := a.applyTLSConfig(config); err != nil {
		return nil, err
	}

	return config, nil
}

func (a *AuthConfig) buildExecAuth() (*rest.Config, error) {
	config := &rest.Config{
		Host: a.ServerURL,
		TLSClientConfig: rest.TLSClientConfig{},
		ExecProvider: &api.ExecConfig{
			Command: a.ExecCommand,
			Args:    a.ExecArgs,
		},
	}

	if len(a.ExecEnv) > 0 {
		var envVars []api.ExecEnvVar
		for key, value := range a.ExecEnv {
			envVars = append(envVars, api.ExecEnvVar{
				Name:  key,
				Value: value,
			})
		}
		config.ExecProvider.Env = envVars
	}

	if err := a.applyTLSConfig(config); err != nil {
		return nil, err
	}

	return config, nil
}

func (a *AuthConfig) applyTLSConfig(config *rest.Config) error {
	if a.Insecure {
		config.TLSClientConfig.Insecure = true
		logger.WithField("insecure", true).Info("TLS verification disabled (insecure mode)")
		return nil
	}

	if a.CAData != "" {
		logger.WithField("ca_data_length", len(a.CAData)).Info("Processing CA certificate data")
		
		// First, try to use the CA data as-is (if it's already PEM encoded)
		if strings.HasPrefix(a.CAData, "-----BEGIN CERTIFICATE-----") {
			// It's already PEM encoded, use as-is
			config.TLSClientConfig.CAData = []byte(a.CAData)
			logger.Info("CA data is already PEM encoded, using as-is")
		} else {
			// Try to decode from base64
			caData, err := base64.StdEncoding.DecodeString(a.CAData)
			if err != nil {
				// If base64 decode fails, try using it as-is (might be plain text)
				logger.WithError(err).Warn("Failed to decode CA data as base64, trying as plain text")
				config.TLSClientConfig.CAData = []byte(a.CAData)
			} else {
				config.TLSClientConfig.CAData = caData
				logger.WithField("ca_data_decoded_length", len(caData)).Info("CA data decoded from base64 successfully")
			}
		}
	} else if a.CAPath != "" {
		config.TLSClientConfig.CAFile = a.CAPath
		logger.WithField("ca_path", a.CAPath).Info("Using CA file path")
	} else {
		logger.Info("No CA data or path provided, using system defaults")
	}

	return nil
}

func CreateServiceAccountConfig(namespace, serviceAccountName string) *AuthConfig {
	tokenPath := fmt.Sprintf("/var/run/secrets/kubernetes.io/serviceaccount/%s/%s/token",
		namespace, serviceAccountName)
	caPath := "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	return &AuthConfig{
		Type:               models.ClusterAuthServiceAccount,
		ServiceAccountPath: tokenPath,
		CAPath:             caPath,
	}
}

func ValidateAuthConfig(ctx context.Context, auth *AuthConfig) error {
	config, err := auth.BuildRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to build REST config: %w", err)
	}

	return ValidateClusterConnection(ctx, config)
}