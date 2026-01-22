package auth

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateKeyFormat(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{"valid key", "ss_0123456789abcdef0123456789abcdef", true},
		{"valid key uppercase", "ss_0123456789ABCDEF0123456789ABCDEF", true},
		{"valid key mixed case", "ss_0123456789AbCdEf0123456789aBcDeF", true},
		{"missing prefix", "0123456789abcdef0123456789abcdef", false},
		{"wrong prefix", "xx_0123456789abcdef0123456789abcdef", false},
		{"too short", "ss_0123456789abcdef", false},
		{"too long", "ss_0123456789abcdef0123456789abcdef00", false},
		{"invalid hex char", "ss_0123456789abcdefghijklmnopqrstuv", false},
		{"empty", "", false},
		{"just prefix", "ss_", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateKeyFormat(tt.key); got != tt.want {
				t.Errorf("ValidateKeyFormat(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestHashKey(t *testing.T) {
	key := "ss_0123456789abcdef0123456789abcdef"
	hash1 := HashKey(key)
	hash2 := HashKey(key)

	// Same input should produce same hash
	if hash1 != hash2 {
		t.Errorf("HashKey() not deterministic: %s != %s", hash1, hash2)
	}

	// Hash should be 64 hex chars (SHA256 = 32 bytes = 64 hex chars)
	if len(hash1) != 64 {
		t.Errorf("HashKey() length = %d, want 64", len(hash1))
	}

	// Different keys should produce different hashes
	differentKey := "ss_fedcba9876543210fedcba9876543210"
	hash3 := HashKey(differentKey)
	if hash1 == hash3 {
		t.Error("Different keys should produce different hashes")
	}
}

func TestSecureCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"same", "same", true},
		{"different", "other", false},
		{"", "", true},
		{"a", "", false},
		{"", "a", false},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			if got := SecureCompare(tt.a, tt.b); got != tt.want {
				t.Errorf("SecureCompare(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestAPIKeyAuthenticator_Authenticate(t *testing.T) {
	validKey := "ss_0123456789abcdef0123456789abcdef"

	auth, err := NewAPIKeyAuthenticator(APIKeyConfig{
		Enabled: true,
		Key:     validKey,
	}, nil)
	if err != nil {
		t.Fatalf("NewAPIKeyAuthenticator() error = %v", err)
	}

	tests := []struct {
		name       string
		headers    map[string]string
		wantID     bool
		wantErr    error
		wantErrNil bool
	}{
		{
			name:       "valid X-API-Key header",
			headers:    map[string]string{"X-API-Key": validKey},
			wantID:     true,
			wantErrNil: true,
		},
		{
			name:       "valid Bearer token",
			headers:    map[string]string{"Authorization": "Bearer " + validKey},
			wantID:     true,
			wantErrNil: true,
		},
		{
			name:       "no credentials",
			headers:    map[string]string{},
			wantID:     false,
			wantErrNil: true,
		},
		{
			name:       "invalid key format",
			headers:    map[string]string{"X-API-Key": "invalid"},
			wantID:     false,
			wantErr:    ErrInvalidKeyFormat,
			wantErrNil: false,
		},
		{
			name:       "wrong key",
			headers:    map[string]string{"X-API-Key": "ss_fedcba9876543210fedcba9876543210"},
			wantID:     false,
			wantErr:    ErrInvalidCredentials,
			wantErrNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			id, err := auth.Authenticate(req)

			if tt.wantErrNil && err != nil {
				t.Errorf("Authenticate() error = %v, want nil", err)
			}
			if !tt.wantErrNil && err == nil {
				t.Errorf("Authenticate() error = nil, want error")
			}
			if tt.wantErr != nil && err != tt.wantErr {
				t.Errorf("Authenticate() error = %v, want %v", err, tt.wantErr)
			}

			if tt.wantID && id == nil {
				t.Error("Authenticate() identity = nil, want identity")
			}
			if !tt.wantID && id != nil {
				t.Errorf("Authenticate() identity = %v, want nil", id)
			}

			if id != nil {
				if id.AuthType != "apikey" {
					t.Errorf("Identity.AuthType = %q, want 'apikey'", id.AuthType)
				}
				if id.Role != RoleOperator { // default role
					t.Errorf("Identity.Role = %v, want RoleOperator", id.Role)
				}
			}
		})
	}
}

func TestAPIKeyAuthenticator_CustomHeader(t *testing.T) {
	validKey := "ss_0123456789abcdef0123456789abcdef"

	auth, err := NewAPIKeyAuthenticator(APIKeyConfig{
		Enabled:    true,
		Key:        validKey,
		HeaderName: "X-Custom-Key",
	}, nil)
	if err != nil {
		t.Fatalf("NewAPIKeyAuthenticator() error = %v", err)
	}

	// Should work with custom header
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Custom-Key", validKey)

	id, err := auth.Authenticate(req)
	if err != nil {
		t.Errorf("Authenticate() error = %v", err)
	}
	if id == nil {
		t.Error("Authenticate() identity = nil, want identity")
	}

	// Should not work with default header
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", validKey)

	id, err = auth.Authenticate(req)
	if err != nil {
		t.Errorf("Authenticate() error = %v", err)
	}
	if id != nil {
		t.Error("Authenticate() with wrong header should return nil identity")
	}
}

func TestAPIKeyAuthenticator_KeyEnv(t *testing.T) {
	validKey := "ss_0123456789abcdef0123456789abcdef"
	envVar := "TEST_API_KEY_12345"

	// Set environment variable
	os.Setenv(envVar, validKey)
	defer os.Unsetenv(envVar)

	auth, err := NewAPIKeyAuthenticator(APIKeyConfig{
		Enabled: true,
		KeyEnv:  envVar,
	}, nil)
	if err != nil {
		t.Fatalf("NewAPIKeyAuthenticator() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", validKey)

	id, err := auth.Authenticate(req)
	if err != nil {
		t.Errorf("Authenticate() error = %v", err)
	}
	if id == nil {
		t.Error("Authenticate() identity = nil, want identity")
	}
}

func TestAPIKeyAuthenticator_KeysFile(t *testing.T) {
	// Create temp file with keys
	tmpDir := t.TempDir()
	keysFile := filepath.Join(tmpDir, "keys.txt")

	keys := []string{
		"# This is a comment",
		"ss_0123456789abcdef0123456789abcdef",
		"ss_fedcba9876543210fedcba9876543210:viewer:viewer-key",
		"ss_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1:admin:admin-key",
		"", // empty line
	}
	content := ""
	for _, k := range keys {
		content += k + "\n"
	}
	if err := os.WriteFile(keysFile, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	auth, err := NewAPIKeyAuthenticator(APIKeyConfig{
		Enabled:  true,
		KeysFile: keysFile,
	}, nil)
	if err != nil {
		t.Fatalf("NewAPIKeyAuthenticator() error = %v", err)
	}

	// Test first key (default role)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "ss_0123456789abcdef0123456789abcdef")

	id, err := auth.Authenticate(req)
	if err != nil {
		t.Errorf("Authenticate() error = %v", err)
	}
	if id == nil {
		t.Fatal("Authenticate() identity = nil, want identity")
	}
	if id.Role != RoleOperator {
		t.Errorf("Identity.Role = %v, want RoleOperator", id.Role)
	}

	// Test viewer key
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "ss_fedcba9876543210fedcba9876543210")

	id, err = auth.Authenticate(req)
	if err != nil {
		t.Errorf("Authenticate() error = %v", err)
	}
	if id == nil {
		t.Fatal("Authenticate() identity = nil, want identity")
	}
	if id.Role != RoleViewer {
		t.Errorf("Identity.Role = %v, want RoleViewer", id.Role)
	}
	if id.Name != "viewer-key" {
		t.Errorf("Identity.Name = %q, want 'viewer-key'", id.Name)
	}

	// Test admin key
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "ss_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1")

	id, err = auth.Authenticate(req)
	if err != nil {
		t.Errorf("Authenticate() error = %v", err)
	}
	if id == nil {
		t.Fatal("Authenticate() identity = nil, want identity")
	}
	if id.Role != RoleAdmin {
		t.Errorf("Identity.Role = %v, want RoleAdmin", id.Role)
	}
}

func TestAPIKeyAuthenticator_NoKeys(t *testing.T) {
	_, err := NewAPIKeyAuthenticator(APIKeyConfig{
		Enabled: true,
	}, nil)

	if err == nil {
		t.Error("NewAPIKeyAuthenticator() with no keys should return error")
	}
}

func TestAPIKeyAuthenticator_InvalidKeyInConfig(t *testing.T) {
	_, err := NewAPIKeyAuthenticator(APIKeyConfig{
		Enabled: true,
		Key:     "invalid",
	}, nil)

	if err == nil {
		t.Error("NewAPIKeyAuthenticator() with invalid key should return error")
	}
}

func TestGenerateAPIKey(t *testing.T) {
	key1, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}

	if !ValidateKeyFormat(key1) {
		t.Errorf("Generated key %q has invalid format", key1)
	}

	// Generate another key, should be different
	key2, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}

	if key1 == key2 {
		t.Error("GenerateAPIKey() should generate different keys")
	}
}

func TestAPIKeyAuthenticator_BearerWithoutSpace(t *testing.T) {
	validKey := "ss_0123456789abcdef0123456789abcdef"

	auth, err := NewAPIKeyAuthenticator(APIKeyConfig{
		Enabled: true,
		Key:     validKey,
	}, nil)
	if err != nil {
		t.Fatalf("NewAPIKeyAuthenticator() error = %v", err)
	}

	// Bearer without space should not match
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer"+validKey)

	id, err := auth.Authenticate(req)
	if err != nil {
		t.Errorf("Authenticate() error = %v", err)
	}
	if id != nil {
		t.Error("Authenticate() with malformed Bearer should return nil identity")
	}
}
