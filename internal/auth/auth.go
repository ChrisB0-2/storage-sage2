// Package auth provides authentication and authorization for storage-sage's HTTP API.
package auth

import (
	"context"
	"errors"
	"net/http"
)

// Role represents the authorization level of an identity.
type Role int

const (
	// RoleNone has no access (used for public paths).
	RoleNone Role = iota
	// RoleViewer can read status and audit data.
	RoleViewer
	// RoleOperator can trigger cleanup runs.
	RoleOperator
	// RoleAdmin has full access (reserved for future admin endpoints).
	RoleAdmin
)

// String returns the string representation of the role.
func (r Role) String() string {
	switch r {
	case RoleNone:
		return "none"
	case RoleViewer:
		return "viewer"
	case RoleOperator:
		return "operator"
	case RoleAdmin:
		return "admin"
	default:
		return "unknown"
	}
}

// ParseRole parses a role string into a Role.
func ParseRole(s string) (Role, error) {
	switch s {
	case "none", "":
		return RoleNone, nil
	case "viewer":
		return RoleViewer, nil
	case "operator":
		return RoleOperator, nil
	case "admin":
		return RoleAdmin, nil
	default:
		return RoleNone, errors.New("unknown role: " + s)
	}
}

// Identity represents an authenticated entity.
type Identity struct {
	// ID is a unique identifier for this identity (e.g., key ID, user ID).
	ID string
	// Name is a human-readable name for this identity.
	Name string
	// Role is the authorization level of this identity.
	Role Role
	// AuthType indicates how this identity was authenticated (e.g., "apikey", "oidc", "mtls").
	AuthType string
}

// contextKey is a private type for context keys to avoid collisions.
type contextKey int

const (
	// ContextKeyIdentity is the context key for storing Identity.
	ContextKeyIdentity contextKey = iota
)

// IdentityFromContext retrieves the Identity from the request context.
// Returns nil if no identity is present.
func IdentityFromContext(ctx context.Context) *Identity {
	if id, ok := ctx.Value(ContextKeyIdentity).(*Identity); ok {
		return id
	}
	return nil
}

// ContextWithIdentity returns a new context with the identity stored.
func ContextWithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, ContextKeyIdentity, id)
}

// Authenticator validates credentials from an HTTP request and returns an identity.
type Authenticator interface {
	// Authenticate attempts to authenticate the request.
	// Returns the identity if authentication succeeds, nil if no credentials were provided,
	// or an error if credentials were provided but invalid.
	Authenticate(r *http.Request) (*Identity, error)
}

// Common authentication errors.
var (
	// ErrNoCredentials indicates no credentials were provided.
	ErrNoCredentials = errors.New("no credentials provided")
	// ErrInvalidCredentials indicates the provided credentials were invalid.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrInvalidKeyFormat indicates the API key format is invalid.
	ErrInvalidKeyFormat = errors.New("invalid API key format")
)
