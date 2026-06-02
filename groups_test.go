package google_oidc_auth_middleware

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGroupsFetcherCreation(t *testing.T) {
	t.Run("fails with invalid JSON", func(t *testing.T) {
		_, err := newGroupsFetcher("invalid json", "admin@example.com")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("fails with missing private key", func(t *testing.T) {
		badJSON := `{"client_email": "test@example.com"}`
		_, err := newGroupsFetcher(badJSON, "admin@example.com")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("fails with invalid private key format", func(t *testing.T) {
		invalidKeyJSON := `{
			"client_email": "test@example.com",
			"private_key": "not-a-valid-pem-key",
			"token_uri": "https://oauth2.googleapis.com/token"
		}`
		_, err := newGroupsFetcher(invalidKeyJSON, "admin@example.com")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestIsGroupMember(t *testing.T) {
	tests := []struct {
		name         string
		userGroups   []string
		allowGroups  map[string]struct{}
		expected     bool
	}{
		{
			name:        "user in allowed group",
			userGroups:  []string{"Developers", "Users"},
			allowGroups: map[string]struct{}{"Developers": {}, "Admins": {}},
			expected:    true,
		},
		{
			name:        "user not in allowed group",
			userGroups:  []string{"Users"},
			allowGroups: map[string]struct{}{"Developers": {}, "Admins": {}},
			expected:    false,
		},
		{
			name:        "no allowed groups configured",
			userGroups:  []string{"Any Group"},
			allowGroups: nil,
			expected:    true,
		},
		{
			name:        "empty allowGroups map",
			userGroups:  []string{"Users"},
			allowGroups: map[string]struct{}{},
			expected:    false,
		},
		{
			name:        "empty user groups",
			userGroups:  []string{},
			allowGroups: map[string]struct{}{"Admins": {}},
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isGroupMember(tt.userGroups, tt.allowGroups)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestAuthCookieWithGroups(t *testing.T) {
	signer := newCookieSigner("test-secret")
	groups := []string{"devs@example.com", "users@example.com"}

	// Create a cookie with groups
	value, err := newAuthCookie(signer, getFutureTime(1), "user@example.com", "example.com", groups)
	if err != nil {
		t.Fatalf("failed to create auth cookie: %v", err)
	}

	// Decode and verify
	var decoded AuthCookie
	if err := json.Unmarshal([]byte(value), &decoded); err == nil {
		t.Errorf("should not be able to unmarshal cookie value directly (it's signed)")
	}

	// Create a request with the cookie and verify it round-trips
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "test", Value: value})

	// Decode using the signer
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		t.Fatalf("expected cookie format to be <sig>.<data>, got %d parts", len(parts))
	}

	dataB64 := parts[1]
	data, err := base64.RawURLEncoding.DecodeString(dataB64)
	if err != nil {
		t.Fatalf("failed to decode cookie data: %v", err)
	}

	var ac AuthCookie
	if err := json.Unmarshal(data, &ac); err != nil {
		t.Fatalf("failed to unmarshal cookie: %v", err)
	}

	if ac.Email != "user@example.com" {
		t.Errorf("expected email user@example.com, got %s", ac.Email)
	}
	if ac.Domain != "example.com" {
		t.Errorf("expected domain example.com, got %s", ac.Domain)
	}
	if len(ac.Groups) != len(groups) {
		t.Errorf("expected %d groups, got %d", len(groups), len(ac.Groups))
	}
	for i, g := range ac.Groups {
		if g != groups[i] {
			t.Errorf("group[%d]: expected %s, got %s", i, groups[i], g)
		}
	}
}

func getFutureTime(hours int) time.Time {
	return time.Now().Add(time.Duration(hours) * time.Hour)
}
