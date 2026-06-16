package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type AuthConfig struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
	StatePath    string
}

type TokenSet struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type AuthManager struct {
	cfg        AuthConfig
	httpClient *http.Client
}

func NewAuthManager(cfg AuthConfig) *AuthManager {
	return &AuthManager{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (m *AuthManager) Refresh(ctx context.Context) (TokenSet, error) {
	refreshToken := strings.TrimSpace(m.cfg.RefreshToken)
	if refreshToken == "" {
		if cached, err := m.load(); err == nil {
			refreshToken = cached.RefreshToken
		}
	}
	if refreshToken == "" {
		return TokenSet{}, errors.New("missing Twitch refresh token")
	}
	if strings.TrimSpace(m.cfg.ClientID) == "" || strings.TrimSpace(m.cfg.ClientSecret) == "" {
		return TokenSet{}, errors.New("missing Twitch client ID or client secret")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", m.cfg.ClientID)
	form.Set("client_secret", m.cfg.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return TokenSet{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return TokenSet{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Message != "" {
			return TokenSet{}, errors.New(apiErr.Message)
		}
		return TokenSet{}, fmt.Errorf("twitch refresh failed with status %s", resp.Status)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return TokenSet{}, err
	}
	if result.AccessToken == "" {
		return TokenSet{}, errors.New("twitch refresh response did not include an access token")
	}
	if result.RefreshToken == "" {
		result.RefreshToken = refreshToken
	}

	tokenSet := TokenSet{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
	}
	m.save(tokenSet)
	return tokenSet, nil
}

func (m *AuthManager) load() (TokenSet, error) {
	if m.cfg.StatePath == "" {
		return TokenSet{}, errors.New("no token state path configured")
	}
	data, err := os.ReadFile(m.cfg.StatePath)
	if err != nil {
		return TokenSet{}, err
	}
	var tokenSet TokenSet
	if err := json.Unmarshal(data, &tokenSet); err != nil {
		return TokenSet{}, err
	}
	return tokenSet, nil
}

func (m *AuthManager) save(tokenSet TokenSet) {
	if m.cfg.StatePath == "" {
		return
	}
	data, err := json.MarshalIndent(tokenSet, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(m.cfg.StatePath, data, 0600)
}
