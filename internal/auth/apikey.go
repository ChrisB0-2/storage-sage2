package auth

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/ChrisB0-2/storage-sage/internal/logger"
)

const (
	// APIKeyPrefix is the required prefix for API keys.
	APIKeyPrefix = "ss_"
	// APIKeyLength is the total length of a valid API key (prefix + 32 hex chars).
	APIKeyLength = 3 + 32 // "ss_" + 32 hex chars

	// DefaultHeaderName is the default header for API key authentication.
	DefaultHeaderName = "X-API-Key"
)

// APIKeyEntry represents a stored API key with its metadata.
type APIKeyEntry struct {
	// Hash is the SHA256 hash of the key (hex-encoded).
	Hash string
	// Name is a human-readable name for this key.
	Name string
	// Role is the authorization level for this key.
	Role Role
}

// APIKeyAuthenticator authenticates requests using API keys.
type APIKeyAuthenticator struct {
	mu         sync.RWMutex
	keys       map[string]APIKeyEntry // hash -> entry
	headerName string
	log        logger.Logger
}

// APIKeyConfig configures the API key authenticator.
type APIKeyConfig struct {
	// Enabled enables API key authentication.
	Enabled bool
	// Key is a single API key (plaintext). For simple setups.
	Key string
	// KeyEnv is the name of an environment variable containing the API key.
	KeyEnv string
	// KeysFile is the path to a file containing multiple keys.
	// Format: one key per line, optionally with "key:role:name" format.
	KeysFile string
	// HeaderName is the header name for API key authentication (default: X-API-Key).
	HeaderName string
	// DefaultRole is the role assigned to keys without an explicit role (default: Operator).
	DefaultRole Role
}

// NewAPIKeyAuthenticator creates a new API key authenticator from configuration.
func NewAPIKeyAuthenticator(cfg APIKeyConfig, log logger.Logger) (*APIKeyAuthenticator, error) {
	if log == nil {
		log = logger.NewNop()
	}

	headerName := cfg.HeaderName
	if headerName == "" {
		headerName = DefaultHeaderName
	}

	defaultRole := cfg.DefaultRole
	if defaultRole == RoleNone {
		defaultRole = RoleOperator
	}

	a := &APIKeyAuthenticator{
		keys:       make(map[string]APIKeyEntry),
		headerName: headerName,
		log:        log,
	}

	// Load key from direct configuration
	if cfg.Key != "" {
		if err := a.addKey(cfg.Key, "config", defaultRole); err != nil {
			return nil, fmt.Errorf("invalid key in config: %w", err)
		}
	}

	// Load key from environment variable
	if cfg.KeyEnv != "" {
		if key := os.Getenv(cfg.KeyEnv); key != "" {
			if err := a.addKey(key, "env:"+cfg.KeyEnv, defaultRole); err != nil {
				return nil, fmt.Errorf("invalid key in env %s: %w", cfg.KeyEnv, err)
			}
		}
	}

	// Load keys from file
	if cfg.KeysFile != "" {
		if err := a.loadKeysFile(cfg.KeysFile, defaultRole); err != nil {
			return nil, fmt.Errorf("failed to load keys file: %w", err)
		}
	}

	if len(a.keys) == 0 {
		return nil, fmt.Errorf("no API keys configured")
	}

	log.Info("API key authenticator initialized", logger.F("key_count", len(a.keys)))

	return a, nil
}

// Authenticate implements Authenticator.
func (a *APIKeyAuthenticator) Authenticate(r *http.Request) (*Identity, error) {
	key := a.extractKey(r)
	if key == "" {
		return nil, nil // No credentials provided
	}

	// Validate key format
	if !ValidateKeyFormat(key) {
		return nil, ErrInvalidKeyFormat
	}

	// Hash the key for lookup
	hash := HashKey(key)

	a.mu.RLock()
	entry, ok := a.keys[hash]
	a.mu.RUnlock()

	if !ok {
		return nil, ErrInvalidCredentials
	}

	return &Identity{
		ID:       hash[:16], // First 16 chars of hash as ID
		Name:     entry.Name,
		Role:     entry.Role,
		AuthType: "apikey",
	}, nil
}

// extractKey extracts the API key from the request.
// Checks X-API-Key header first, then Authorization: Bearer.
func (a *APIKeyAuthenticator) extractKey(r *http.Request) string {
	// Check custom header first
	if key := r.Header.Get(a.headerName); key != "" {
		return key
	}

	// Check Authorization header with Bearer scheme
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	return ""
}

// addKey adds a key to the authenticator.
func (a *APIKeyAuthenticator) addKey(key, name string, role Role) error {
	if !ValidateKeyFormat(key) {
		return ErrInvalidKeyFormat
	}

	hash := HashKey(key)

	a.mu.Lock()
	defer a.mu.Unlock()

	a.keys[hash] = APIKeyEntry{
		Hash: hash,
		Name: name,
		Role: role,
	}

	return nil
}

// loadKeysFile loads keys from a file.
// File format: one entry per line
// Simple format: ss_<hex> (uses default role)
// Extended format: ss_<hex>:role:name
func (a *APIKeyAuthenticator) loadKeysFile(path string, defaultRole Role) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse line
		parts := strings.SplitN(line, ":", 3)
		key := parts[0]
		role := defaultRole
		name := fmt.Sprintf("file:%s:%d", path, lineNum)

		if len(parts) >= 2 && parts[1] != "" {
			r, err := ParseRole(parts[1])
			if err != nil {
				return fmt.Errorf("line %d: %w", lineNum, err)
			}
			role = r
		}

		if len(parts) >= 3 && parts[2] != "" {
			name = parts[2]
		}

		if err := a.addKey(key, name, role); err != nil {
			return fmt.Errorf("line %d: %w", lineNum, err)
		}
	}

	return scanner.Err()
}

// ValidateKeyFormat checks if a key has the correct format.
// Valid format: "ss_" prefix followed by exactly 32 hex characters.
func ValidateKeyFormat(key string) bool {
	if len(key) != APIKeyLength {
		return false
	}
	if !strings.HasPrefix(key, APIKeyPrefix) {
		return false
	}

	// Check that the rest is valid hex
	hexPart := key[len(APIKeyPrefix):]
	for _, c := range hexPart {
		if !isHexChar(c) {
			return false
		}
	}

	return true
}

// isHexChar returns true if c is a valid hexadecimal character.
func isHexChar(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// HashKey computes the SHA256 hash of a key and returns it as a hex string.
func HashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// SecureCompare performs a constant-time comparison of two strings.
// Returns true if they are equal.
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// GenerateAPIKey generates a new cryptographically secure random API key.
func GenerateAPIKey() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return APIKeyPrefix + hex.EncodeToString(bytes), nil
}
