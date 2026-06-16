package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type StreamInfo struct {
	Channel     string
	Title       string
	GameName    string
	ViewerCount int
	StartedAt   time.Time
	Live        bool
}

type HelixClient struct {
	clientID    string
	accessToken string
	httpClient  *http.Client
}

func NewHelixClient(clientID, accessToken string) *HelixClient {
	return &HelixClient{
		clientID:    clientID,
		accessToken: trimOAuthPrefix(accessToken),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *HelixClient) GetStreamInfo(ctx context.Context, channel string) (StreamInfo, error) {
	endpoint := "https://api.twitch.tv/helix/streams?user_login=" + url.QueryEscape(channel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return StreamInfo{}, err
	}
	req.Header.Set("Client-Id", c.clientID)
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return StreamInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return StreamInfo{}, fmt.Errorf("helix streams request failed with status %s", resp.Status)
	}

	var result struct {
		Data []struct {
			UserLogin   string    `json:"user_login"`
			Title       string    `json:"title"`
			GameName    string    `json:"game_name"`
			ViewerCount int       `json:"viewer_count"`
			StartedAt   time.Time `json:"started_at"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return StreamInfo{}, err
	}

	if len(result.Data) == 0 {
		return StreamInfo{Channel: channel, Live: false}, nil
	}

	stream := result.Data[0]
	return StreamInfo{
		Channel:     stream.UserLogin,
		Title:       stream.Title,
		GameName:    stream.GameName,
		ViewerCount: stream.ViewerCount,
		StartedAt:   stream.StartedAt,
		Live:        true,
	}, nil
}

func trimOAuthPrefix(token string) string {
	if len(token) >= len("oauth:") && token[:len("oauth:")] == "oauth:" {
		return token[len("oauth:"):]
	}
	return token
}
