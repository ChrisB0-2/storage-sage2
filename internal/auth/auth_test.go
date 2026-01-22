package auth

import (
	"context"
	"testing"
)

func TestRole_String(t *testing.T) {
	tests := []struct {
		role Role
		want string
	}{
		{RoleNone, "none"},
		{RoleViewer, "viewer"},
		{RoleOperator, "operator"},
		{RoleAdmin, "admin"},
		{Role(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.role.String(); got != tt.want {
				t.Errorf("Role.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseRole(t *testing.T) {
	tests := []struct {
		input   string
		want    Role
		wantErr bool
	}{
		{"none", RoleNone, false},
		{"", RoleNone, false},
		{"viewer", RoleViewer, false},
		{"operator", RoleOperator, false},
		{"admin", RoleAdmin, false},
		{"invalid", RoleNone, true},
		{"VIEWER", RoleNone, true}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseRole(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRole(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseRole(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIdentityFromContext(t *testing.T) {
	// Test with no identity
	ctx := context.Background()
	if got := IdentityFromContext(ctx); got != nil {
		t.Errorf("IdentityFromContext(empty) = %v, want nil", got)
	}

	// Test with identity
	id := &Identity{
		ID:       "test-id",
		Name:     "test-user",
		Role:     RoleOperator,
		AuthType: "apikey",
	}
	ctx = ContextWithIdentity(ctx, id)

	got := IdentityFromContext(ctx)
	if got == nil {
		t.Fatal("IdentityFromContext() = nil, want identity")
	}
	if got.ID != id.ID {
		t.Errorf("Identity.ID = %q, want %q", got.ID, id.ID)
	}
	if got.Name != id.Name {
		t.Errorf("Identity.Name = %q, want %q", got.Name, id.Name)
	}
	if got.Role != id.Role {
		t.Errorf("Identity.Role = %v, want %v", got.Role, id.Role)
	}
	if got.AuthType != id.AuthType {
		t.Errorf("Identity.AuthType = %q, want %q", got.AuthType, id.AuthType)
	}
}

func TestContextWithIdentity_Nil(t *testing.T) {
	ctx := context.Background()
	ctx = ContextWithIdentity(ctx, nil)

	// Should return nil when nil was stored
	if got := IdentityFromContext(ctx); got != nil {
		t.Errorf("IdentityFromContext(nil stored) = %v, want nil", got)
	}
}
