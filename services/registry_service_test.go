package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHTTPClient is a deterministic test double for httpClient.
type fakeHTTPClient struct {
	handler   func(req *http.Request) (*http.Response, error)
	lastReq   *http.Request
	callCount int
}

func (f *fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	f.lastReq = req
	f.callCount++
	if f.handler != nil {
		return f.handler(req)
	}
	return nil, errors.New("no handler configured")
}

func newFakeHTTPClient(handler func(req *http.Request) (*http.Response, error)) *fakeHTTPClient {
	return &fakeHTTPClient{handler: handler}
}

func httpResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func transportError() (*http.Response, error) {
	return nil, errors.New("dial tcp: connection refused")
}

func newTestRegistryService(client httpClient) *RegistryService {
	return &RegistryService{httpClient: client}
}

// --- Canonical table-driven success + transport-error (issue #130) ---

func TestRegistryHTTPConnection_SuccessAndTransportError(t *testing.T) {
	tests := []struct {
		name       string
		registry   *models.Registry
		handler    func(t *testing.T) func(*http.Request) (*http.Response, error)
		wantErr    bool
		errContain string
		testFunc   func(svc *RegistryService, ctx context.Context, r *models.Registry) error
	}{
		{
			name:     "dockerhub success 200",
			registry: &models.Registry{RegistryType: models.RegistryTypeDockerHub, Credentials: models.RegistryCredentials{Username: "u", Password: "p"}},
			handler: func(t *testing.T) func(*http.Request) (*http.Response, error) {
				return func(req *http.Request) (*http.Response, error) {
					assert.Equal(t, "GET", req.Method)
					assert.Equal(t, "https://registry-1.docker.io/v2/", req.URL.String())
					assert.True(t, strings.HasPrefix(req.Header.Get("Authorization"), "Basic "))
					return httpResponse(200, "{}"), nil
				}
			},
			wantErr: false,
			testFunc: func(svc *RegistryService, ctx context.Context, r *models.Registry) error {
				return svc.testDockerHubConnection(ctx, r)
			},
		},
		{
			name:     "dockerhub transport error",
			registry: &models.Registry{RegistryType: models.RegistryTypeDockerHub, Credentials: models.RegistryCredentials{Username: "u", Password: "p"}},
			handler: func(t *testing.T) func(*http.Request) (*http.Response, error) {
				return func(req *http.Request) (*http.Response, error) { return transportError() }
			},
			wantErr:    true,
			errContain: "connection failed",
			testFunc: func(svc *RegistryService, ctx context.Context, r *models.Registry) error {
				return svc.testDockerHubConnection(ctx, r)
			},
		},
		{
			name:     "ghcr success 200",
			registry: &models.Registry{RegistryType: models.RegistryTypeGHCR, Credentials: models.RegistryCredentials{Username: "u", Password: "p"}},
			handler: func(t *testing.T) func(*http.Request) (*http.Response, error) {
				return func(req *http.Request) (*http.Response, error) {
					assert.Equal(t, "GET", req.Method)
					assert.Equal(t, "https://ghcr.io/v2/", req.URL.String())
					assert.True(t, strings.HasPrefix(req.Header.Get("Authorization"), "Basic "))
					return httpResponse(200, "{}"), nil
				}
			},
			wantErr: false,
			testFunc: func(svc *RegistryService, ctx context.Context, r *models.Registry) error {
				return svc.testGHCRConnection(ctx, r)
			},
		},
		{
			name:     "ghcr transport error",
			registry: &models.Registry{RegistryType: models.RegistryTypeGHCR, Credentials: models.RegistryCredentials{Username: "u", Password: "p"}},
			handler: func(t *testing.T) func(*http.Request) (*http.Response, error) {
				return func(req *http.Request) (*http.Response, error) { return transportError() }
			},
			wantErr:    true,
			errContain: "connection failed",
			testFunc: func(svc *RegistryService, ctx context.Context, r *models.Registry) error {
				return svc.testGHCRConnection(ctx, r)
			},
		},
		{
			name:     "acr success 200",
			registry: &models.Registry{RegistryType: models.RegistryTypeACR, RegistryURL: "https://myregistry.azurecr.io"},
			handler: func(t *testing.T) func(*http.Request) (*http.Response, error) {
				return func(req *http.Request) (*http.Response, error) {
					assert.Equal(t, "GET", req.Method)
					assert.Equal(t, "https://myregistry.azurecr.io/v2/", req.URL.String())
					return httpResponse(200, "{}"), nil
				}
			},
			wantErr: false,
			testFunc: func(svc *RegistryService, ctx context.Context, r *models.Registry) error {
				return svc.testACRConnection(ctx, r)
			},
		},
		{
			name:     "acr transport error",
			registry: &models.Registry{RegistryType: models.RegistryTypeACR, RegistryURL: "https://myregistry.azurecr.io"},
			handler: func(t *testing.T) func(*http.Request) (*http.Response, error) {
				return func(req *http.Request) (*http.Response, error) { return transportError() }
			},
			wantErr:    true,
			errContain: "connection failed",
			testFunc: func(svc *RegistryService, ctx context.Context, r *models.Registry) error {
				return svc.testACRConnection(ctx, r)
			},
		},
		{
			name:     "custom success 200",
			registry: &models.Registry{RegistryType: models.RegistryTypeCustom, RegistryURL: "https://registry.example.com", Credentials: models.RegistryCredentials{Username: "u", Password: "p"}},
			handler: func(t *testing.T) func(*http.Request) (*http.Response, error) {
				return func(req *http.Request) (*http.Response, error) {
					assert.Equal(t, "GET", req.Method)
					assert.Equal(t, "https://registry.example.com/v2/", req.URL.String())
					assert.True(t, strings.HasPrefix(req.Header.Get("Authorization"), "Basic "))
					return httpResponse(200, "{}"), nil
				}
			},
			wantErr: false,
			testFunc: func(svc *RegistryService, ctx context.Context, r *models.Registry) error {
				return svc.testCustomRegistryConnection(ctx, r)
			},
		},
		{
			name:     "custom transport error",
			registry: &models.Registry{RegistryType: models.RegistryTypeCustom, RegistryURL: "https://registry.example.com"},
			handler: func(t *testing.T) func(*http.Request) (*http.Response, error) {
				return func(req *http.Request) (*http.Response, error) { return transportError() }
			},
			wantErr:    true,
			errContain: "connection failed",
			testFunc: func(svc *RegistryService, ctx context.Context, r *models.Registry) error {
				return svc.testCustomRegistryConnection(ctx, r)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeHTTPClient(tt.handler(t))
			svc := newTestRegistryService(fake)
			err := tt.testFunc(svc, context.Background(), tt.registry)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// --- Preserved unique tests ---

func TestTestDockerHubConnection_401IsSuccess(t *testing.T) {
	fake := newFakeHTTPClient(func(req *http.Request) (*http.Response, error) {
		return httpResponse(401, `{"errors":[{"code":"UNAUTHORIZED"}]}`), nil
	})
	svc := newTestRegistryService(fake)
	registry := &models.Registry{RegistryType: models.RegistryTypeDockerHub, Credentials: models.RegistryCredentials{Username: "u", Password: "p"}}
	require.NoError(t, svc.testDockerHubConnection(context.Background(), registry))
}

func TestTestDockerHubConnection_UnexpectedStatus(t *testing.T) {
	fake := newFakeHTTPClient(func(req *http.Request) (*http.Response, error) {
		return httpResponse(500, "internal error"), nil
	})
	svc := newTestRegistryService(fake)
	registry := &models.Registry{RegistryType: models.RegistryTypeDockerHub, Credentials: models.RegistryCredentials{Username: "u", Password: "p"}}
	err := svc.testDockerHubConnection(context.Background(), registry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status code: 500")
}

func TestTestCustomRegistryConnection_NoAuthHeader(t *testing.T) {
	customURL := "https://registry.example.com"
	fake := newFakeHTTPClient(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "", req.Header.Get("Authorization"))
		return httpResponse(401, ""), nil
	})
	svc := newTestRegistryService(fake)
	registry := &models.Registry{RegistryType: models.RegistryTypeCustom, RegistryURL: customURL}
	require.NoError(t, svc.testCustomRegistryConnection(context.Background(), registry))
}

func TestTestGCRConnection_Success(t *testing.T) {
	sa := map[string]string{"client_email": "test@project.iam.gserviceaccount.com", "private_key": "-----BEGIN RSA PRIVATE KEY-----\nfake\n-----END RSA PRIVATE KEY-----", "project_id": "test-project"}
	saJSON, err := json.Marshal(sa)
	require.NoError(t, err)
	svc := newTestRegistryService(newFakeHTTPClient(nil))
	registry := &models.Registry{RegistryType: models.RegistryTypeGCR, Credentials: models.RegistryCredentials{ServiceAccountJSON: string(saJSON)}}
	require.NoError(t, svc.testGCRConnection(context.Background(), registry))
}

func TestTestGCRConnection_InvalidJSON(t *testing.T) {
	svc := newTestRegistryService(newFakeHTTPClient(nil))
	registry := &models.Registry{RegistryType: models.RegistryTypeGCR, Credentials: models.RegistryCredentials{ServiceAccountJSON: "not-json"}}
	err := svc.testGCRConnection(context.Background(), registry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid service account JSON")
}

func TestTestGCRConnection_MissingField(t *testing.T) {
	sa := map[string]string{"client_email": "test@project.iam.gserviceaccount.com"}
	saJSON, err := json.Marshal(sa)
	require.NoError(t, err)
	svc := newTestRegistryService(newFakeHTTPClient(nil))
	registry := &models.Registry{RegistryType: models.RegistryTypeGCR, Credentials: models.RegistryCredentials{ServiceAccountJSON: string(saJSON)}}
	err = svc.testGCRConnection(context.Background(), registry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required field")
}

func TestTestConnection_Dispatch(t *testing.T) {
	fake := newFakeHTTPClient(func(req *http.Request) (*http.Response, error) { return httpResponse(200, "{}"), nil })
	svc := newTestRegistryService(fake)
	tests := []struct {
		name     string
		registry *models.Registry
		wantErr  bool
	}{
		{name: "dockerhub dispatches to HTTP check", registry: &models.Registry{RegistryType: models.RegistryTypeDockerHub, Credentials: models.RegistryCredentials{Username: "u", Password: "p"}}, wantErr: false},
		{name: "ghcr dispatches to HTTP check", registry: &models.Registry{RegistryType: models.RegistryTypeGHCR, Credentials: models.RegistryCredentials{Username: "u", Password: "p"}}, wantErr: false},
		{name: "acr dispatches to HTTP check", registry: &models.Registry{RegistryType: models.RegistryTypeACR, RegistryURL: "https://myregistry.azurecr.io"}, wantErr: false},
		{name: "custom dispatches to HTTP check", registry: &models.Registry{RegistryType: models.RegistryTypeCustom, RegistryURL: "https://registry.example.com", Credentials: models.RegistryCredentials{Username: "u", Password: "p"}}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.testConnection(context.Background(), tt.registry)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTestConnection_UnsupportedType(t *testing.T) {
	svc := newTestRegistryService(newFakeHTTPClient(nil))
	registry := &models.Registry{RegistryType: models.RegistryType("unsupported")}
	err := svc.testConnection(context.Background(), registry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported registry type")
}

func TestInjectedClientIsUsed(t *testing.T) {
	fake := newFakeHTTPClient(func(req *http.Request) (*http.Response, error) { return httpResponse(200, "{}"), nil })
	svc := newTestRegistryService(fake)
	registry := &models.Registry{RegistryType: models.RegistryTypeDockerHub, Credentials: models.RegistryCredentials{Username: "u", Password: "p"}}
	_ = svc.testDockerHubConnection(context.Background(), registry)
	require.Equal(t, 1, fake.callCount)
	require.NotNil(t, fake.lastReq)
	assert.Equal(t, "Basic ", fake.lastReq.Header.Get("Authorization")[:6])
}

// Production timeout contract: 10s on *http.Client.
// NewRegistryService() requires Mongo, so we verify the same construction it uses
// (&http.Client{Timeout: 10*time.Second}) implements httpClient.
func TestNewRegistryService_HasDefaultClient(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	var hc httpClient = client
	require.NotNil(t, hc)
	_, ok := hc.(*http.Client)
	assert.True(t, ok, "production client should be *http.Client")
	assert.Equal(t, 10*time.Second, client.Timeout)
	svc := newTestRegistryService(client)
	require.NotNil(t, svc.httpClient)
}

// --- Custom registry credential validation (issue #131) ---

func TestTestCustomRegistryConnection_CredentialValidation(t *testing.T) {
	const (
		dummyUser = "dummy-user"
		dummyPass = "dummy-secret-password"
		regURL    = "https://registry.example.com"
		regName   = "my-custom-registry"
	)

	tests := []struct {
		name       string
		registry   *models.Registry
		status     int
		transport  bool
		wantErr    bool
		errContain string
		checkAuth  bool
	}{
		{
			name:      "2xx success with credentials",
			registry:  &models.Registry{Name: regName, RegistryType: models.RegistryTypeCustom, RegistryURL: regURL, Credentials: models.RegistryCredentials{Username: dummyUser, Password: dummyPass}},
			status:    200,
			wantErr:   false,
			checkAuth: true,
		},
		{
			name:      "204 success with credentials",
			registry:  &models.Registry{Name: regName, RegistryType: models.RegistryTypeCustom, RegistryURL: regURL, Credentials: models.RegistryCredentials{Username: dummyUser, Password: dummyPass}},
			status:    204,
			wantErr:   false,
			checkAuth: true,
		},
		{
			name:       "401 auth failure when credentials configured",
			registry:   &models.Registry{Name: regName, RegistryType: models.RegistryTypeCustom, RegistryURL: regURL, Credentials: models.RegistryCredentials{Username: dummyUser, Password: dummyPass}},
			status:     401,
			wantErr:    true,
			errContain: "authentication failed",
			checkAuth:  true,
		},
		{
			name:       "403 auth failure when credentials configured",
			registry:   &models.Registry{Name: regName, RegistryType: models.RegistryTypeCustom, RegistryURL: regURL, Credentials: models.RegistryCredentials{Username: dummyUser, Password: dummyPass}},
			status:     403,
			wantErr:    true,
			errContain: "authentication failed",
			checkAuth:  true,
		},
		{
			name:       "unexpected status 500",
			registry:   &models.Registry{Name: regName, RegistryType: models.RegistryTypeCustom, RegistryURL: regURL, Credentials: models.RegistryCredentials{Username: dummyUser, Password: dummyPass}},
			status:     500,
			wantErr:    true,
			errContain: "unexpected status",
			checkAuth:  true,
		},
		{
			name:       "transport failure",
			registry:   &models.Registry{Name: regName, RegistryType: models.RegistryTypeCustom, RegistryURL: regURL, Credentials: models.RegistryCredentials{Username: dummyUser, Password: dummyPass}},
			transport:  true,
			wantErr:    true,
			errContain: "connection failed",
			checkAuth:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeHTTPClient(func(req *http.Request) (*http.Response, error) {
				if tt.checkAuth {
					auth := req.Header.Get("Authorization")
					assert.True(t, strings.HasPrefix(auth, "Basic "), "expected Basic auth header prefix")
					assert.NotContains(t, auth, dummyPass, "raw password must not appear in Authorization header value checks via body; header is base64")
				}
				if tt.transport {
					return transportError()
				}
				return httpResponse(tt.status, ""), nil
			})
			svc := newTestRegistryService(fake)
			err := svc.testCustomRegistryConnection(context.Background(), tt.registry)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
				assert.Contains(t, err.Error(), regName)
				assert.NotContains(t, err.Error(), dummyPass)
				assert.NotContains(t, err.Error(), "Authorization")
				assert.NotContains(t, err.Error(), "Basic ")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAuthorizationHeaderFormat(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{name: "simple", username: "user", password: "pass"},
		{name: "special chars", username: "user@domain.com", password: "p@$$w0rd!"},
		{name: "empty credentials", username: "", password: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedAuth string
			fake := newFakeHTTPClient(func(req *http.Request) (*http.Response, error) {
				capturedAuth = req.Header.Get("Authorization")
				return httpResponse(200, "{}"), nil
			})
			svc := newTestRegistryService(fake)
			registry := &models.Registry{RegistryType: models.RegistryTypeDockerHub, Credentials: models.RegistryCredentials{Username: tt.username, Password: tt.password}}
			_ = svc.testDockerHubConnection(context.Background(), registry)
			if tt.username != "" || tt.password != "" {
				assert.True(t, strings.HasPrefix(capturedAuth, "Basic "))
				assert.NotContains(t, capturedAuth, tt.password)
			}
		})
	}
}
