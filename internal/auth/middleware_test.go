package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockAuthenticator implements Authenticator for testing.
type mockAuthenticator struct {
	identity *Identity
	err      error
}

func (m *mockAuthenticator) Authenticate(_ *http.Request) (*Identity, error) {
	return m.identity, m.err
}

func TestMiddleware_PublicPath(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// Create middleware with /health as public
	m := NewMiddleware(nil, nil, []string{"/health"})
	wrapped := m.Wrap(handler)

	// Request to public path should pass through without auth
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler was not called for public path")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestMiddleware_NoCredentials(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// Authenticator returns nil, nil (no credentials)
	auth := &mockAuthenticator{identity: nil, err: nil}
	m := NewMiddleware(nil, []Authenticator{auth}, nil)
	wrapped := m.Wrap(handler)

	req := httptest.NewRequest("GET", "/protected", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if called {
		t.Error("Handler should not be called when no credentials")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_InvalidCredentials(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// Authenticator returns error (invalid credentials)
	auth := &mockAuthenticator{identity: nil, err: errors.New("invalid")}
	m := NewMiddleware(nil, []Authenticator{auth}, nil)
	wrapped := m.Wrap(handler)

	req := httptest.NewRequest("GET", "/protected", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if called {
		t.Error("Handler should not be called when credentials are invalid")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_ValidCredentials(t *testing.T) {
	var receivedIdentity *Identity
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedIdentity = IdentityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	identity := &Identity{
		ID:       "test-id",
		Name:     "test-user",
		Role:     RoleOperator,
		AuthType: "test",
	}
	auth := &mockAuthenticator{identity: identity, err: nil}
	m := NewMiddleware(nil, []Authenticator{auth}, nil)
	wrapped := m.Wrap(handler)

	req := httptest.NewRequest("GET", "/protected", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
	if receivedIdentity == nil {
		t.Fatal("Identity not passed to handler")
	}
	if receivedIdentity.ID != identity.ID {
		t.Errorf("Identity.ID = %q, want %q", receivedIdentity.ID, identity.ID)
	}
}

func TestMiddleware_MultipleAuthenticators(t *testing.T) {
	var receivedIdentity *Identity
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedIdentity = IdentityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	// First authenticator returns no credentials
	auth1 := &mockAuthenticator{identity: nil, err: nil}
	// Second authenticator returns valid identity
	identity := &Identity{ID: "second", Name: "second-auth", Role: RoleViewer, AuthType: "second"}
	auth2 := &mockAuthenticator{identity: identity, err: nil}

	m := NewMiddleware(nil, []Authenticator{auth1, auth2}, nil)
	wrapped := m.Wrap(handler)

	req := httptest.NewRequest("GET", "/protected", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
	if receivedIdentity == nil {
		t.Fatal("Identity not passed to handler")
	}
	if receivedIdentity.ID != "second" {
		t.Errorf("Identity.ID = %q, want 'second'", receivedIdentity.ID)
	}
}

func TestMiddleware_FirstAuthenticatorError(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// First authenticator returns error
	auth1 := &mockAuthenticator{identity: nil, err: errors.New("invalid")}
	// Second authenticator would succeed but shouldn't be called
	identity := &Identity{ID: "second", Name: "second-auth", Role: RoleViewer, AuthType: "second"}
	auth2 := &mockAuthenticator{identity: identity, err: nil}

	m := NewMiddleware(nil, []Authenticator{auth1, auth2}, nil)
	wrapped := m.Wrap(handler)

	req := httptest.NewRequest("GET", "/protected", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if called {
		t.Error("Handler should not be called when first authenticator errors")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_IsPublicPath(t *testing.T) {
	m := NewMiddleware(nil, nil, []string{"/health", "/ready"})

	tests := []struct {
		path string
		want bool
	}{
		{"/health", true},
		{"/ready", true},
		{"/status", false},
		{"/health/", false}, // exact match
		{"/", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := m.IsPublicPath(tt.path); got != tt.want {
				t.Errorf("IsPublicPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestMiddleware_JSONErrorResponse(t *testing.T) {
	auth := &mockAuthenticator{identity: nil, err: nil}
	m := NewMiddleware(nil, []Authenticator{auth}, nil)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := m.Wrap(handler)

	req := httptest.NewRequest("GET", "/protected", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want 'application/json'", rec.Header().Get("Content-Type"))
	}

	body := rec.Body.String()
	if body == "" {
		t.Error("Response body is empty")
	}
	// Should contain error message
	if !contains(body, "error") {
		t.Errorf("Response should contain 'error': %s", body)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
