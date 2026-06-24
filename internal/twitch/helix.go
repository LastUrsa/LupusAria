package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type ChatMessageResult struct {
	MessageID   string
	IsSent      bool
	DropCode    string
	DropMessage string
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

func (c *HelixClient) IsChannelFollower(ctx context.Context, broadcasterID, userID string) (bool, error) {
	values := url.Values{}
	values.Set("broadcaster_id", strings.TrimSpace(broadcasterID))
	values.Set("user_id", strings.TrimSpace(userID))

	endpoint := "https://api.twitch.tv/helix/channels/followers?" + values.Encode()
	var result struct {
		Data []struct {
			UserID string `json:"user_id"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, endpoint, &result); err != nil {
		return false, err
	}
	return len(result.Data) > 0, nil
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
			NextAdAt        flexibleTime    `json:"next_ad_at"`
			LastAdAt        flexibleTime    `json:"last_ad_at"`
			Duration        flexibleInteger `json:"duration"`
			PrerollFreeTime flexibleInteger `json:"preroll_free_time"`
			SnoozeCount     flexibleInteger `json:"snooze_count"`
			SnoozeRefreshAt flexibleTime    `json:"snooze_refresh_at"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, endpoint, &result); err != nil {
		return AdSchedule{}, err
	}
	if len(result.Data) == 0 {
		return AdSchedule{}, nil
	}

	item := result.Data[0]
	return AdSchedule{
		NextAdAt:        item.NextAdAt.Time,
		LastAdAt:        item.LastAdAt.Time,
		Duration:        time.Duration(item.Duration) * time.Second,
		PrerollFreeTime: time.Duration(item.PrerollFreeTime) * time.Second,
		SnoozeCount:     int(item.SnoozeCount),
		SnoozeRefreshAt: item.SnoozeRefreshAt.Time,
	}, nil
}

func (c *HelixClient) CreateEventSubWebSocketSubscription(ctx context.Context, subscriptionType, version string, condition map[string]string, sessionID string) error {
	if strings.TrimSpace(subscriptionType) == "" {
		return errors.New("missing EventSub subscription type")
	}
	if strings.TrimSpace(version) == "" {
		version = "1"
	}
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("missing EventSub WebSocket session ID")
	}
	payload := struct {
		Type      string            `json:"type"`
		Version   string            `json:"version"`
		Condition map[string]string `json:"condition"`
		Transport struct {
			Method    string `json:"method"`
			SessionID string `json:"session_id"`
		} `json:"transport"`
	}{
		Type:      subscriptionType,
		Version:   version,
		Condition: condition,
	}
	payload.Transport.Method = "websocket"
	payload.Transport.SessionID = sessionID

	var result any
	return c.postJSON(ctx, "https://api.twitch.tv/helix/eventsub/subscriptions", payload, &result)
}

func (c *HelixClient) SendChatMessage(ctx context.Context, broadcasterID, senderID, message, replyParentMessageID string) (ChatMessageResult, error) {
	payload := struct {
		BroadcasterID        string `json:"broadcaster_id"`
		SenderID             string `json:"sender_id"`
		Message              string `json:"message"`
		ReplyParentMessageID string `json:"reply_parent_message_id,omitempty"`
	}{
		BroadcasterID:        strings.TrimSpace(broadcasterID),
		SenderID:             strings.TrimSpace(senderID),
		Message:              sanitizeChatAPIMessage(message),
		ReplyParentMessageID: strings.TrimSpace(replyParentMessageID),
	}
	if payload.BroadcasterID == "" || payload.SenderID == "" {
		return ChatMessageResult{}, errors.New("missing broadcaster or sender ID")
	}
	if payload.Message == "" {
		return ChatMessageResult{}, errors.New("missing chat message")
	}

	var result struct {
		Data []struct {
			MessageID  string `json:"message_id"`
			IsSent     bool   `json:"is_sent"`
			DropReason *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"drop_reason"`
		} `json:"data"`
	}
	if err := c.postJSON(ctx, "https://api.twitch.tv/helix/chat/messages", payload, &result); err != nil {
		return ChatMessageResult{}, err
	}
	if len(result.Data) == 0 {
		return ChatMessageResult{}, errors.New("send chat message response did not include data")
	}
	item := result.Data[0]
	out := ChatMessageResult{MessageID: item.MessageID, IsSent: item.IsSent}
	if item.DropReason != nil {
		out.DropCode = item.DropReason.Code
		out.DropMessage = item.DropReason.Message
	}
	if !out.IsSent {
		if out.DropMessage != "" {
			return out, fmt.Errorf("twitch dropped chat message: %s", out.DropMessage)
		}
		return out, errors.New("twitch dropped chat message")
	}
	return out, nil
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
		return helixStatusError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *HelixClient) postJSON(ctx context.Context, endpoint string, payload, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Client-Id", c.clientID)
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return helixStatusError(resp)
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func helixStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	detail := strings.Join(strings.Fields(string(body)), " ")
	if detail == "" {
		return fmt.Errorf("helix request failed with status %s", resp.Status)
	}
	return fmt.Errorf("helix request failed with status %s: %s", resp.Status, detail)
}

func sanitizeChatAPIMessage(message string) string {
	message = strings.ReplaceAll(message, "\r", " ")
	message = strings.ReplaceAll(message, "\n", " ")
	return strings.Join(strings.Fields(message), " ")
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

type flexibleTime struct {
	time.Time
}

func parseOptionalTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	var parsed flexibleTime
	if err := parsed.UnmarshalJSON([]byte(strconv.Quote(raw))); err != nil {
		return time.Time{}, err
	}
	return parsed.Time, nil
}

func (t *flexibleTime) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" || raw == "0" {
		t.Time = time.Time{}
		return nil
	}
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		asString = strings.TrimSpace(asString)
		if asString == "" || asString == "0" {
			t.Time = time.Time{}
			return nil
		}
		if parsed, err := time.Parse(time.RFC3339, asString); err == nil {
			t.Time = parsed
			return nil
		}
		raw = asString
	}
	epochSeconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return err
	}
	if epochSeconds <= 0 {
		t.Time = time.Time{}
		return nil
	}
	t.Time = time.Unix(epochSeconds, 0).UTC()
	return nil
}

func trimOAuthPrefix(token string) string {
	if len(token) >= len("oauth:") && token[:len("oauth:")] == "oauth:" {
		return token[len("oauth:"):]
	}
	return token
}
