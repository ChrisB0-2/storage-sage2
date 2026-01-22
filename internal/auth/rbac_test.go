package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRBACMiddleware_DefaultPermissions(t *testing.T) {
	perms := DefaultPermissions()

	// Verify expected permissions exist
	expected := map[string]struct {
		method  string
		minRole Role
	}{
		"/ready":      {"GET", RoleViewer},
		"/status":     {"GET", RoleViewer},
		"/api/config": {"GET", RoleViewer},
		"/api/audit/": {"GET", RoleViewer},
		"/trigger":    {"POST", RoleOperator},
		"/":           {"GET", RoleViewer},
	}

	for path, exp := range expected {
		found := false
		for _, p := range perms {
			if p.PathPrefix == path && p.Method == exp.method {
				found = true
				if p.MinRole != exp.minRole {
					t.Errorf("Permission for %s %s has MinRole %v, want %v",
						exp.method, path, p.MinRole, exp.minRole)
				}
				break
			}
		}
		if !found {
			t.Errorf("Missing permission for %s %s", exp.method, path)
		}
	}
}

func TestRBACMiddleware_Wrap(t *testing.T) {
	perms := []Permission{
		{PathPrefix: "/viewer", Method: "GET", MinRole: RoleViewer},
		{PathPrefix: "/operator", Method: "POST", MinRole: RoleOperator},
		{PathPrefix: "/admin", Method: "", MinRole: RoleAdmin}, // any method
	}

	tests := []struct {
		name       string
		path       string
		method     string
		role       Role
		wantStatus int
	}{
		// Viewer role tests
		{"viewer can access /viewer GET", "/viewer", "GET", RoleViewer, http.StatusOK},
		{"operator can access /viewer GET", "/viewer", "GET", RoleOperator, http.StatusOK},
		{"admin can access /viewer GET", "/viewer", "GET", RoleAdmin, http.StatusOK},

		// Operator role tests
		{"viewer cannot access /operator POST", "/operator", "POST", RoleViewer, http.StatusForbidden},
		{"operator can access /operator POST", "/operator", "POST", RoleOperator, http.StatusOK},
		{"admin can access /operator POST", "/operator", "POST", RoleAdmin, http.StatusOK},

		// Admin role tests
		{"viewer cannot access /admin", "/admin", "GET", RoleViewer, http.StatusForbidden},
		{"operator cannot access /admin", "/admin", "GET", RoleOperator, http.StatusForbidden},
		{"admin can access /admin GET", "/admin", "GET", RoleAdmin, http.StatusOK},
		{"admin can access /admin POST", "/admin", "POST", RoleAdmin, http.StatusOK},

		// Undefined path
		{"undefined path denied", "/undefined", "GET", RoleAdmin, http.StatusForbidden},

		// Method mismatch
		{"viewer cannot POST to /viewer", "/viewer", "POST", RoleViewer, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewRBACMiddleware(perms, nil)

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			wrapped := m.Wrap(handler)

			// Create request with identity in context
			req := httptest.NewRequest(tt.method, tt.path, nil)
			identity := &Identity{ID: "test", Name: "test", Role: tt.role, AuthType: "test"}
			req = req.WithContext(ContextWithIdentity(req.Context(), identity))

			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestRBACMiddleware_NoIdentity(t *testing.T) {
	perms := []Permission{
		{PathPrefix: "/test", Method: "GET", MinRole: RoleViewer},
	}
	m := NewRBACMiddleware(perms, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := m.Wrap(handler)

	// Request without identity
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRBACMiddleware_LongestPrefixMatch(t *testing.T) {
	perms := []Permission{
		{PathPrefix: "/api/", Method: "GET", MinRole: RoleViewer},
		{PathPrefix: "/api/admin/", Method: "GET", MinRole: RoleAdmin},
	}
	m := NewRBACMiddleware(perms, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := m.Wrap(handler)

	// Viewer should access /api/status
	req := httptest.NewRequest("GET", "/api/status", nil)
	identity := &Identity{ID: "test", Name: "test", Role: RoleViewer, AuthType: "test"}
	req = req.WithContext(ContextWithIdentity(req.Context(), identity))
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("/api/status with viewer: Status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Viewer should NOT access /api/admin/users (longer prefix matches)
	req = httptest.NewRequest("GET", "/api/admin/users", nil)
	req = req.WithContext(ContextWithIdentity(req.Context(), identity))
	rec = httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("/api/admin/users with viewer: Status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	// Admin should access /api/admin/users
	identity = &Identity{ID: "test", Name: "test", Role: RoleAdmin, AuthType: "test"}
	req = httptest.NewRequest("GET", "/api/admin/users", nil)
	req = req.WithContext(ContextWithIdentity(req.Context(), identity))
	rec = httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("/api/admin/users with admin: Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRBACMiddleware_HasPermission(t *testing.T) {
	perms := []Permission{
		{PathPrefix: "/viewer", Method: "GET", MinRole: RoleViewer},
		{PathPrefix: "/operator", Method: "POST", MinRole: RoleOperator},
	}
	m := NewRBACMiddleware(perms, nil)

	tests := []struct {
		name   string
		role   Role
		path   string
		method string
		want   bool
	}{
		{"viewer can GET /viewer", RoleViewer, "/viewer", "GET", true},
		{"viewer cannot POST /operator", RoleViewer, "/operator", "POST", false},
		{"operator can POST /operator", RoleOperator, "/operator", "POST", true},
		{"nil identity", RoleNone, "/viewer", "GET", false},
		{"unknown path", RoleAdmin, "/unknown", "GET", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var identity *Identity
			if tt.name != "nil identity" {
				identity = &Identity{ID: "test", Name: "test", Role: tt.role, AuthType: "test"}
			}

			got := m.HasPermission(identity, tt.path, tt.method)
			if got != tt.want {
				t.Errorf("HasPermission() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRBACMiddleware_JSONErrorResponse(t *testing.T) {
	perms := []Permission{
		{PathPrefix: "/test", Method: "GET", MinRole: RoleAdmin},
	}
	m := NewRBACMiddleware(perms, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := m.Wrap(handler)

	// Request with insufficient permissions
	req := httptest.NewRequest("GET", "/test", nil)
	identity := &Identity{ID: "test", Name: "test", Role: RoleViewer, AuthType: "test"}
	req = req.WithContext(ContextWithIdentity(req.Context(), identity))
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want 'application/json'", rec.Header().Get("Content-Type"))
	}
}
