package auth

import (
	"net/http"
	"strings"

	"github.com/ChrisB0-2/storage-sage/internal/logger"
)

// Permission defines the required role for an endpoint.
type Permission struct {
	// PathPrefix is the URL path prefix this permission applies to.
	PathPrefix string
	// Method is the HTTP method (empty string matches all methods).
	Method string
	// MinRole is the minimum role required to access this endpoint.
	MinRole Role
}

// RBACMiddleware enforces role-based access control.
type RBACMiddleware struct {
	permissions []Permission
	log         logger.Logger
}

// NewRBACMiddleware creates a new RBAC middleware with the given permissions.
func NewRBACMiddleware(permissions []Permission, log logger.Logger) *RBACMiddleware {
	if log == nil {
		log = logger.NewNop()
	}
	return &RBACMiddleware{
		permissions: permissions,
		log:         log,
	}
}

// DefaultPermissions returns the default permission set for storage-sage.
func DefaultPermissions() []Permission {
	return []Permission{
		// Health endpoint is public (handled by auth middleware public paths)
		// These are the role requirements for authenticated users

		// Read-only endpoints require Viewer role
		{PathPrefix: "/ready", Method: "GET", MinRole: RoleViewer},
		{PathPrefix: "/status", Method: "GET", MinRole: RoleViewer},
		{PathPrefix: "/api/config", Method: "GET", MinRole: RoleViewer},
		{PathPrefix: "/api/audit/", Method: "GET", MinRole: RoleViewer},

		// Trigger endpoint requires Operator role
		{PathPrefix: "/trigger", Method: "POST", MinRole: RoleOperator},

		// Static files (frontend) require Viewer role
		{PathPrefix: "/", Method: "GET", MinRole: RoleViewer},
	}
}

// Wrap returns an HTTP handler that enforces role-based access control.
func (m *RBACMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := IdentityFromContext(r.Context())

		// Find the matching permission
		perm := m.findPermission(r.URL.Path, r.Method)
		if perm == nil {
			// No explicit permission defined - deny by default
			m.log.Warn("no permission defined for endpoint",
				logger.F("path", r.URL.Path),
				logger.F("method", r.Method),
			)
			writeJSONError(w, http.StatusForbidden, "access denied")
			return
		}

		// Check if identity meets the minimum role requirement
		if identity == nil {
			// This shouldn't happen if auth middleware ran first, but be safe
			writeJSONError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		if identity.Role < perm.MinRole {
			m.log.Warn("insufficient permissions",
				logger.F("path", r.URL.Path),
				logger.F("method", r.Method),
				logger.F("identity", identity.Name),
				logger.F("role", identity.Role.String()),
				logger.F("required", perm.MinRole.String()),
			)
			writeJSONError(w, http.StatusForbidden, "insufficient permissions")
			return
		}

		// Access granted
		next.ServeHTTP(w, r)
	})
}

// findPermission finds the permission that matches the given path and method.
// Returns nil if no matching permission is found.
func (m *RBACMiddleware) findPermission(path, method string) *Permission {
	// Find the most specific matching permission (longest prefix match)
	var bestMatch *Permission
	bestLen := -1

	for i := range m.permissions {
		p := &m.permissions[i]

		// Check if path matches
		if !strings.HasPrefix(path, p.PathPrefix) {
			continue
		}

		// Check if method matches (empty matches all)
		if p.Method != "" && p.Method != method {
			continue
		}

		// Use longest prefix match
		if len(p.PathPrefix) > bestLen {
			bestMatch = p
			bestLen = len(p.PathPrefix)
		}
	}

	return bestMatch
}

// HasPermission checks if the given identity has permission for the path and method.
func (m *RBACMiddleware) HasPermission(identity *Identity, path, method string) bool {
	if identity == nil {
		return false
	}

	perm := m.findPermission(path, method)
	if perm == nil {
		return false
	}

	return identity.Role >= perm.MinRole
}
