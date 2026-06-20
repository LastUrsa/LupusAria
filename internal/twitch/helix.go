package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

type UserInfo struct {
	ID              string
	Login           string
	DisplayName     string
	BroadcasterType string
}

type Chatter struct {
	UserID    string
	UserLogin string
	UserName  string
}

type ChannelEmote struct {
	ID   string
	Name string
}

type AdSchedule struct {
	NextAdAt        time.Time
	LastAdAt        time.Time
	Duration        time.Duration
	PrerollFreeTime time.Duration
	SnoozeCount     int
	SnoozeRefreshAt time.Time
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

func (c *HelixClient) SetAccessToken(accessToken string) {
	c.accessToken = trimOAuthPrefix(accessToken)
}

func (c *HelixClient) GetStreamInfo(ctx context.Context, channel string) (StreamInfo, error) {
	endpoint := "https://api.twitch.tv/helix/streams?user_login=" + url.QueryEscape(channel)
	var result struct {
		Data []struct {
			UserLogin   string    `json:"user_login"`
			Title       string    `json:"title"`
			GameName    string    `json:"game_name"`
			ViewerCount int       `json:"viewer_count"`
			StartedAt   time.Time `json:"started_at"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, endpoint, &result); err != nil {
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

func (c *HelixClient) GetUsersByLogin(ctx context.Context, logins []string) ([]UserInfo, error) {
	if len(logins) == 0 {
		return nil, nil
	}

	values := url.Values{}
	for _, login := range logins {
		login = strings.TrimSpace(strings.TrimPrefix(login, "@"))
		if login != "" {
			values.Add("login", strings.ToLower(login))
		}
	}
	if len(values["login"]) == 0 {
		return nil, nil
	}

	endpoint := "https://api.twitch.tv/helix/users?" + values.Encode()
	var result struct {
		Data []struct {
			ID              string `json:"id"`
			Login           string `json:"login"`
			DisplayName     string `json:"display_name"`
			BroadcasterType string `json:"broadcaster_type"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, endpoint, &result); err != nil {
		return nil, err
	}

	users := make([]UserInfo, 0, len(result.Data))
	for _, item := range result.Data {
		users = append(users, UserInfo{
			ID:              item.ID,
			Login:           item.Login,
			DisplayName:     item.DisplayName,
			BroadcasterType: item.BroadcasterType,
		})
	}
	return users, nil
}

func (c *HelixClient) GetRecentStream(ctx context.Context, userID string) (time.Time, bool, error) {
	values := url.Values{}
	values.Set("user_id", userID)
	values.Set("type", "archive")
	values.Set("first", "1")

	endpoint := "https://api.twitch.tv/helix/videos?" + values.Encode()
	var result struct {
		Data []struct {
			CreatedAt time.Time `json:"created_at"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, endpoint, &result); err != nil {
		return time.Time{}, false, err
	}
	if len(result.Data) == 0 {
		return time.Time{}, false, nil
	}
	return result.Data[0].CreatedAt, true, nil
}

func (c *HelixClient) GetChatters(ctx context.Context, broadcasterID, moderatorID string) ([]Chatter, error) {
	var chatters []Chatter
	cursor := ""

	for {
		values := url.Values{}
		values.Set("broadcaster_id", broadcasterID)
		values.Set("moderator_id", moderatorID)
		values.Set("first", "1000")
		if cursor != "" {
			values.Set("after", cursor)
		}

		endpoint := "https://api.twitch.tv/helix/chat/chatters?" + values.Encode()
		var result struct {
			Data []struct {
				UserID    string `json:"user_id"`
				UserLogin string `json:"user_login"`
				UserName  string `json:"user_name"`
			} `json:"data"`
			Pagination struct {
				Cursor string `json:"cursor"`
			} `json:"pagination"`
		}
		if err := c.getJSON(ctx, endpoint, &result); err != nil {
			return nil, err
		}
		for _, item := range result.Data {
			chatters = append(chatters, Chatter{
				UserID:    item.UserID,
				UserLogin: item.UserLogin,
				UserName:  item.UserName,
			})
		}
		if result.Pagination.Cursor == "" {
			return chatters, nil
		}
		cursor = result.Pagination.Cursor
	}
}

func (c *HelixClient) GetChannelEmotes(ctx context.Context, broadcasterID string) ([]ChannelEmote, error) {
	values := url.Values{}
	values.Set("broadcaster_id", strings.TrimSpace(broadcasterID))

	endpoint := "https://api.twitch.tv/helix/chat/emotes?" + values.Encode()
	var result struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, endpoint, &result); err != nil {
		return nil, err
	}
	emotes := make([]ChannelEmote, 0, len(result.Data))
	for _, item := range result.Data {
		if item.ID == "" || item.Name == "" {
			continue
		}
		emotes = append(emotes, ChannelEmote{ID: item.ID, Name: item.Name})
	}
	return emotes, nil
}

func (c *HelixClient) GetAdSchedule(ctx context.Context, broadcasterID string) (AdSchedule, error) {
	values := url.Values{}
	values.Set("broadcaster_id", broadcasterID)

	endpoint := "https://api.twitch.tv/helix/channels/ads?" + values.Encode()
	var result struct {
		Data []struct {
			NextAdAt        string          `json:"next_ad_at"`
			LastAdAt        string          `json:"last_ad_at"`
			Duration        flexibleInteger `json:"duration"`
			PrerollFreeTime flexibleInteger `json:"preroll_free_time"`
			SnoozeCount     flexibleInteger `json:"snooze_count"`
			SnoozeRefreshAt string          `json:"snooze_refresh_at"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, endpoint, &result); err != nil {
		return AdSchedule{}, err
	}
	if len(result.Data) == 0 {
		return AdSchedule{}, nil
	}

	item := result.Data[0]
	nextAdAt, err := parseOptionalTime(item.NextAdAt)
	if err != nil {
		return AdSchedule{}, fmt.Errorf("parse next_ad_at: %w", err)
	}
	lastAdAt, err := parseOptionalTime(item.LastAdAt)
	if err != nil {
		return AdSchedule{}, fmt.Errorf("parse last_ad_at: %w", err)
	}
	snoozeRefreshAt, err := parseOptionalTime(item.SnoozeRefreshAt)
	if err != nil {
		return AdSchedule{}, fmt.Errorf("parse snooze_refresh_at: %w", err)
	}

	return AdSchedule{
		NextAdAt:        nextAdAt,
		LastAdAt:        lastAdAt,
		Duration:        time.Duration(item.Duration) * time.Second,
		PrerollFreeTime: time.Duration(item.PrerollFreeTime) * time.Second,
		SnoozeCount:     int(item.SnoozeCount),
		SnoozeRefreshAt: snoozeRefreshAt,
	}, nil
}

func (c *HelixClient) getJSON(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Client-Id", c.clientID)
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("helix request failed with status %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

type flexibleInteger int

func (i *flexibleInteger) UnmarshalJSON(data []byte) error {
	var asInt int
	if err := json.Unmarshal(data, &asInt); err == nil {
		*i = flexibleInteger(asInt)
		return nil
	}

	var asString string
	if err := json.Unmarshal(data, &asString); err != nil {
		return err
	}
	if asString == "" {
		*i = 0
		return nil
	}
	parsed, err := strconv.Atoi(asString)
	if err != nil {
		return err
	}
	*i = flexibleInteger(parsed)
	return nil
}

func parseOptionalTime(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func trimOAuthPrefix(token string) string {
	if len(token) >= len("oauth:") && token[:len("oauth:")] == "oauth:" {
		return token[len("oauth:"):]
	}
	return token
}
