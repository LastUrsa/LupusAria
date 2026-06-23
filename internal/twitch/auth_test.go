package twitch

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

func TestAuthManagerAppAccessTokenUsesCachedToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app-token.json")
	manager := NewAuthManager(AuthConfig{StatePath: path})
	manager.save(TokenSet{
		AccessToken: "cached-app-access",
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	manager.httpClient = &http.Client{Transport: authRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected token request")
		return nil, nil
	})}

	tokenSet, err := manager.AppAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tokenSet.AccessToken != "cached-app-access" {
		t.Fatalf("token = %#v", tokenSet)
	}
}

func TestAuthManagerAppAccessTokenRequestsAndCachesClientCredentialsToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app-token.json")
	manager := NewAuthManager(AuthConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		StatePath:    path,
	})
	manager.httpClient = &http.Client{Transport: authRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != "https://id.twitch.tv/oauth2/token" {
			t.Fatalf("request = %s %s", req.Method, req.URL.String())
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		if values.Get("grant_type") != "client_credentials" || values.Get("client_id") != "client-id" || values.Get("client_secret") != "client-secret" {
			t.Fatalf("form = %s", string(body))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"app-access","expires_in":3600}`)),
		}, nil
	})}

	tokenSet, err := manager.AppAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tokenSet.AccessToken != "app-access" || tokenSet.ExpiresAt.IsZero() {
		t.Fatalf("tokenSet = %#v", tokenSet)
	}
	cached, err := manager.load()
	if err != nil {
		t.Fatal(err)
	}
	if cached.AccessToken != "app-access" {
		t.Fatalf("cached = %#v", cached)
	}
}

type authRoundTripFunc func(*http.Request) (*http.Response, error)

func (f authRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
