package twitch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuthManagerSaveWritesPrivateTokenState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")
	manager := NewAuthManager(AuthConfig{StatePath: path})

	manager.save(TokenSet{
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresAt:    time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
	})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}

	tokenSet, err := manager.load()
	if err != nil {
		t.Fatal(err)
	}
	if tokenSet.AccessToken != "access" || tokenSet.RefreshToken != "refresh" {
		t.Fatalf("tokenSet = %#v", tokenSet)
	}
}
