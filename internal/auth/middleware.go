package auth

import (
	"encoding/json"
	"net/http"

	"github.com/ChrisB0-2/storage-sage/internal/logger"
)

// Middleware provides HTTP middleware for authentication.
type Middleware struct {
	authenticators []Authenticator
	publicPaths    map[string]bool
	log            logger.Logger
}

// NewMiddleware creates a new authentication middleware.
func NewMiddleware(log logger.Logger, authenticators []Authenticator, publicPaths []string) *Middleware {
	if log == nil {
		log = logger.NewNop()
	}

	pathMap := make(map[string]bool, len(publicPaths))
	for _, p := range publicPaths {
		pathMap[p] = true
	}

	return &Middleware{
		authenticators: authenticators,
		publicPaths:    pathMap,
		log:            log,
	}
}

// Wrap returns an HTTP handler that enforces authentication.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication for public paths
		if m.publicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		// Try each authenticator in order
		for _, auth := range m.authenticators {
			identity, err := auth.Authenticate(r)
			if identity != nil {
				// Authentication succeeded
				m.log.Debug("request authenticated",
					logger.F("path", r.URL.Path),
					logger.F("identity", identity.Name),
					logger.F("role", identity.Role.String()),
					logger.F("auth_type", identity.AuthType),
				)
				ctx := ContextWithIdentity(r.Context(), identity)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if err != nil {
				// Credentials were provided but invalid
				m.log.Warn("authentication failed",
					logger.F("path", r.URL.Path),
					logger.F("error", err.Error()),
					logger.F("remote_addr", r.RemoteAddr),
				)
				writeJSONError(w, http.StatusUnauthorized, "authentication failed: "+err.Error())
				return
			}
			// No credentials from this authenticator, try next
		}

		// No authenticator could authenticate the request
		m.log.Debug("no credentials provided",
			logger.F("path", r.URL.Path),
			logger.F("remote_addr", r.RemoteAddr),
		)
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
	})
}

// IsPublicPath returns true if the path is configured as public.
func (m *Middleware) IsPublicPath(path string) bool {
	return m.publicPaths[path]
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]string{"error": message}
	_ = json.NewEncoder(w).Encode(resp)
}
