package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const eventSubWebSocketURL = "wss://eventsub.wss.twitch.tv/ws"

type EventSubConfig struct {
	ClientID       string
	Token          string
	SendToken      string
	AdClientID     string
	AdToken        string
	RedeemClientID string
	RedeemToken    string
	Channel        string
	BroadcasterID  string
	UserID         string
	AdBreaks       chan<- AdBreakEvent
	Redeems        chan<- ChannelPointRedeemEvent
}

type AdBreakEvent struct {
	StartedAt time.Time
	Duration  time.Duration
	Automatic bool
}

type ChannelPointRedeemEvent struct {
	ID          string
	RewardID    string
	RewardTitle string
	UserID      string
	UserLogin   string
	UserName    string
	UserInput   string
	RedeemedAt  time.Time
}

type EventSubChatClient struct {
	cfg    EventSubConfig
	helix  *HelixClient
	sender *HelixClient
	logger *slog.Logger

	wsURL  string
	dialer *websocket.Dialer

	mu   sync.Mutex
	conn *websocket.Conn
	aux  *websocket.Conn
}

func NewEventSubChatClient(cfg EventSubConfig, logger *slog.Logger) *EventSubChatClient {
	return &EventSubChatClient{
		cfg:    cfg,
		helix:  NewHelixClient(cfg.ClientID, cfg.Token),
		sender: NewHelixClient(cfg.ClientID, firstNonEmpty(cfg.SendToken, cfg.Token)),
		logger: logger,
		wsURL:  eventSubWebSocketURL,
		dialer: websocket.DefaultDialer,
	}
}

func (c *EventSubChatClient) Connect(ctx context.Context) (<-chan Message, error) {
	if err := c.validateConfig(); err != nil {
		return nil, err
	}

	conn, session, err := c.connectSession(ctx, c.wsURL)
	if err != nil {
		return nil, err
	}
	if err := c.subscribeToChat(ctx, session.ID); err != nil {
		_ = conn.Close()
		return nil, err
	}

	c.setConn(conn)
	messages := make(chan Message, 64)
	go c.readLoop(ctx, conn, messages, c.subscribeToChat, c.setConn, "EventSub chat", true)

	if c.needsBroadcasterSession() {
		auxConn, auxSession, err := c.connectSession(ctx, c.wsURL)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		if err := c.subscribeToBroadcasterEvents(ctx, auxSession.ID); err != nil {
			_ = auxConn.Close()
			_ = conn.Close()
			return nil, err
		}
		c.setAuxConn(auxConn)
		go c.readLoop(ctx, auxConn, nil, c.subscribeToBroadcasterEvents, c.setAuxConn, "EventSub broadcaster", false)
		if c.logger != nil {
			c.logger.Info("connected to Twitch broadcaster EventSub", "channel", c.cfg.Channel, "session_id", auxSession.ID)
		}
	}

	if c.logger != nil {
		c.logger.Info("connected to twitch chat through EventSub", "channel", c.cfg.Channel, "session_id", session.ID)
	}
	return messages, nil
}

func (c *EventSubChatClient) Say(channel, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := c.sender.SendChatMessage(ctx, c.cfg.BroadcasterID, c.cfg.UserID, text, "")
	return err
}

func (c *EventSubChatClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var err error
	if c.conn != nil {
		err = c.conn.Close()
	}
	if c.aux != nil {
		if auxErr := c.aux.Close(); err == nil {
			err = auxErr
		}
	}
	return err
}

func (c *EventSubChatClient) validateConfig() error {
	if strings.TrimSpace(c.cfg.ClientID) == "" {
		return errors.New("missing Twitch client ID")
	}
	if strings.TrimSpace(c.cfg.Token) == "" {
		return errors.New("missing Twitch access token")
	}
	if strings.TrimSpace(c.cfg.BroadcasterID) == "" {
		return errors.New("missing Twitch broadcaster user ID")
	}
	if strings.TrimSpace(c.cfg.UserID) == "" {
		return errors.New("missing Twitch bot user ID")
	}
	return nil
}

func (c *EventSubChatClient) subscribeToChat(ctx context.Context, sessionID string) error {
	condition := map[string]string{
		"broadcaster_user_id": c.cfg.BroadcasterID,
		"user_id":             c.cfg.UserID,
	}
	return c.helix.CreateEventSubWebSocketSubscription(ctx, "channel.chat.message", "1", condition, sessionID)
}

func (c *EventSubChatClient) subscribeToAdBreaks(ctx context.Context, sessionID string) error {
	if c.cfg.AdBreaks == nil {
		return nil
	}
	if strings.TrimSpace(c.cfg.AdToken) == "" {
		return errors.New("missing Twitch ads access token")
	}
	condition := map[string]string{
		"broadcaster_user_id": c.cfg.BroadcasterID,
	}
	helix := NewHelixClient(firstNonEmpty(c.cfg.AdClientID, c.cfg.ClientID), c.cfg.AdToken)
	return helix.CreateEventSubWebSocketSubscription(ctx, "channel.ad_break.begin", "1", condition, sessionID)
}

func (c *EventSubChatClient) subscribeToRedeems(ctx context.Context, sessionID string) error {
	if c.cfg.Redeems == nil {
		return nil
	}
	if strings.TrimSpace(c.cfg.RedeemToken) == "" {
		return errors.New("missing Twitch broadcaster access token")
	}
	condition := map[string]string{
		"broadcaster_user_id": c.cfg.BroadcasterID,
	}
	helix := NewHelixClient(firstNonEmpty(c.cfg.RedeemClientID, c.cfg.AdClientID, c.cfg.ClientID), c.cfg.RedeemToken)
	if err := helix.CreateEventSubWebSocketSubscription(ctx, "channel.channel_points_custom_reward_redemption.add", "1", condition, sessionID); err != nil {
		return err
	}
	if c.logger != nil {
		c.logger.Info("EventSub channel point redeem subscription active")
	}
	return nil
}

func (c *EventSubChatClient) subscribeToBroadcasterEvents(ctx context.Context, sessionID string) error {
	if err := c.subscribeToRedeems(ctx, sessionID); err != nil {
		return fmt.Errorf("subscribe to channel point redeems: %w", err)
	}
	if err := c.subscribeToAdBreaks(ctx, sessionID); err != nil && c.logger != nil {
		c.logger.Warn("EventSub ad break subscription unavailable; ad alerts will use schedule polling", "error", err)
	}
	return nil
}

func (c *EventSubChatClient) needsBroadcasterSession() bool {
	return c.cfg.Redeems != nil || c.cfg.AdBreaks != nil
}

func (c *EventSubChatClient) connectSession(ctx context.Context, rawURL string) (*websocket.Conn, eventSubSession, error) {
	dialer := c.dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, resp, err := dialer.DialContext(ctx, rawURL, http.Header{})
	if err != nil {
		if resp != nil {
			return nil, eventSubSession{}, fmt.Errorf("connect EventSub WebSocket: %w: %s", err, resp.Status)
		}
		return nil, eventSubSession{}, fmt.Errorf("connect EventSub WebSocket: %w", err)
	}

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			_ = conn.Close()
			return nil, eventSubSession{}, err
		}
		msg, err := parseEventSubMessage(data)
		if err != nil {
			_ = conn.Close()
			return nil, eventSubSession{}, err
		}
		if msg.Metadata.MessageType == "session_welcome" {
			if msg.Payload.Session.ID == "" {
				_ = conn.Close()
				return nil, eventSubSession{}, errors.New("EventSub welcome did not include a session ID")
			}
			return conn, msg.Payload.Session, nil
		}
	}
}

type eventSubSubscribeFunc func(context.Context, string) error
type eventSubSetConnFunc func(*websocket.Conn)

func (c *EventSubChatClient) readLoop(ctx context.Context, conn *websocket.Conn, messages chan<- Message, subscribe eventSubSubscribeFunc, setConn eventSubSetConnFunc, label string, closeMessages bool) {
	if closeMessages {
		defer close(messages)
	}
	defer func() {
		_ = conn.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, io.EOF) {
				return
			}
			if c.logger != nil {
				c.logger.Warn(label+" read failed; reconnecting", "error", err)
			}
			next, err := c.reconnectFresh(ctx, subscribe, setConn, label)
			if err != nil {
				if c.logger != nil {
					c.logger.Warn(label+" reconnect failed", "error", err)
				}
				return
			}
			_ = conn.Close()
			conn = next
			continue
		}

		msg, err := parseEventSubMessage(data)
		if err != nil {
			if c.logger != nil {
				c.logger.Warn("failed to parse EventSub message", "error", err)
			}
			continue
		}

		switch msg.Metadata.MessageType {
		case "session_keepalive":
			continue
		case "session_reconnect":
			if msg.Payload.Session.ReconnectURL == "" {
				continue
			}
			next, _, err := c.connectSession(ctx, msg.Payload.Session.ReconnectURL)
			if err != nil {
				if c.logger != nil {
					c.logger.Warn("failed to follow EventSub reconnect URL", "error", err)
				}
				continue
			}
			_ = conn.Close()
			conn = next
			setConn(conn)
		case "notification":
			subscriptionType := firstNonEmpty(msg.Metadata.SubscriptionType, msg.Payload.Subscription.Type)
			switch subscriptionType {
			case "channel.chat.message":
				chatMsg, ok := eventSubChatMessageToMessage(msg.Payload.Event, string(data))
				if ok && messages != nil {
					messages <- chatMsg
				}
			case "channel.ad_break.begin":
				if adBreak, ok := eventSubAdBreakToEvent(msg.Payload.Event); ok {
					select {
					case c.cfg.AdBreaks <- adBreak:
					default:
						if c.logger != nil {
							c.logger.Warn("EventSub ad break event dropped; receiver is busy")
						}
					}
				}
			case "channel.channel_points_custom_reward_redemption.add":
				if redeem, ok := eventSubRedeemToEvent(msg.Payload.Event); ok {
					if c.logger != nil {
						c.logger.Info("EventSub channel point redeem received", "reward", redeem.RewardTitle, "user", redeem.UserLogin)
					}
					select {
					case c.cfg.Redeems <- redeem:
					default:
						if c.logger != nil {
							c.logger.Warn("EventSub channel point redeem event dropped; receiver is busy")
						}
					}
				} else if c.logger != nil {
					c.logger.Warn("EventSub channel point redeem payload could not be parsed")
				}
			}
		case "revocation":
			if c.logger != nil {
				c.logger.Warn("EventSub chat subscription revoked", "type", msg.Payload.Subscription.Type, "status", msg.Payload.Subscription.Status)
			}
		}
	}
}

func (c *EventSubChatClient) reconnectFresh(ctx context.Context, subscribe eventSubSubscribeFunc, setConn eventSubSetConnFunc, label string) (*websocket.Conn, error) {
	backoff := time.Second
	for {
		conn, session, err := c.connectSession(ctx, c.wsURL)
		if err == nil {
			if err := subscribe(ctx, session.ID); err != nil {
				_ = conn.Close()
				err = fmt.Errorf("resubscribe %s: %w", label, err)
			} else {
				setConn(conn)
				return conn, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (c *EventSubChatClient) setConn(conn *websocket.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn = conn
}

func (c *EventSubChatClient) setAuxConn(conn *websocket.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.aux = conn
}

type eventSubMessage struct {
	Metadata eventSubMetadata `json:"metadata"`
	Payload  eventSubPayload  `json:"payload"`
}

type eventSubMetadata struct {
	MessageType         string `json:"message_type"`
	SubscriptionType    string `json:"subscription_type"`
	SubscriptionVersion string `json:"subscription_version"`
}

type eventSubPayload struct {
	Session      eventSubSession      `json:"session"`
	Subscription eventSubSubscription `json:"subscription"`
	Event        eventSubChatEvent    `json:"event"`
}

type eventSubSession struct {
	ID                      string `json:"id"`
	Status                  string `json:"status"`
	KeepaliveTimeoutSeconds int    `json:"keepalive_timeout_seconds"`
	ReconnectURL            string `json:"reconnect_url"`
}

type eventSubSubscription struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type eventSubChatEvent struct {
	BroadcasterUserLogin string          `json:"broadcaster_user_login"`
	ChatterUserID        string          `json:"chatter_user_id"`
	ChatterUserLogin     string          `json:"chatter_user_login"`
	ChatterUserName      string          `json:"chatter_user_name"`
	ID                   string          `json:"id"`
	UserID               string          `json:"user_id"`
	UserLogin            string          `json:"user_login"`
	UserName             string          `json:"user_name"`
	UserInput            string          `json:"user_input"`
	MessageID            string          `json:"message_id"`
	DurationSeconds      flexibleInteger `json:"duration_seconds"`
	StartedAt            string          `json:"started_at"`
	RedeemedAt           string          `json:"redeemed_at"`
	IsAutomatic          flexibleBool    `json:"is_automatic"`
	Reward               struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"reward"`
	Message struct {
		Text      string                 `json:"text"`
		Fragments []eventSubChatFragment `json:"fragments"`
	} `json:"message"`
	Badges []eventSubBadge `json:"badges"`
	Reply  *struct {
		ParentMessageID   string `json:"parent_message_id"`
		ParentMessageBody string `json:"parent_message_body"`
		ParentUserID      string `json:"parent_user_id"`
		ParentUserLogin   string `json:"parent_user_login"`
		ParentUserName    string `json:"parent_user_name"`
	} `json:"reply"`
}

type eventSubChatFragment struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emote *struct {
		ID string `json:"id"`
	} `json:"emote"`
}

type eventSubBadge struct {
	SetID string `json:"set_id"`
	ID    string `json:"id"`
	Info  string `json:"info"`
}

func parseEventSubMessage(data []byte) (eventSubMessage, error) {
	var msg eventSubMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return eventSubMessage{}, err
	}
	return msg, nil
}

func eventSubChatMessageToMessage(event eventSubChatEvent, raw string) (Message, bool) {
	if event.MessageID == "" || event.ChatterUserLogin == "" {
		return Message{}, false
	}
	isBroadcaster := eventSubHasBadge(event.Badges, "broadcaster") || strings.EqualFold(event.ChatterUserLogin, event.BroadcasterUserLogin)
	isMod := isBroadcaster || eventSubHasBadge(event.Badges, "moderator")
	msg := Message{
		ID:            event.MessageID,
		Channel:       strings.ToLower(event.BroadcasterUserLogin),
		UserID:        event.ChatterUserID,
		Username:      strings.ToLower(event.ChatterUserLogin),
		DisplayName:   event.ChatterUserName,
		Text:          event.Message.Text,
		Raw:           raw,
		Emotes:        eventSubFragmentsEmotes(event.Message.Fragments),
		IsBroadcaster: isBroadcaster,
		IsMod:         isMod,
	}
	if msg.DisplayName == "" {
		msg.DisplayName = msg.Username
	}
	if msg.Channel == "" {
		return Message{}, false
	}
	if event.Reply != nil {
		msg.ReplyParentDisplayName = event.Reply.ParentUserName
		msg.ReplyParentUserLogin = strings.ToLower(event.Reply.ParentUserLogin)
		msg.ReplyParentText = event.Reply.ParentMessageBody
	}
	return msg, true
}

func eventSubAdBreakToEvent(event eventSubChatEvent) (AdBreakEvent, bool) {
	if event.DurationSeconds <= 0 {
		return AdBreakEvent{}, false
	}
	startedAt, err := parseOptionalTime(event.StartedAt)
	if err != nil {
		return AdBreakEvent{}, false
	}
	return AdBreakEvent{
		StartedAt: startedAt,
		Duration:  time.Duration(event.DurationSeconds) * time.Second,
		Automatic: bool(event.IsAutomatic),
	}, true
}

func eventSubRedeemToEvent(event eventSubChatEvent) (ChannelPointRedeemEvent, bool) {
	if event.ID == "" || event.Reward.ID == "" {
		return ChannelPointRedeemEvent{}, false
	}
	redeemedAt, err := parseOptionalTime(event.RedeemedAt)
	if err != nil {
		return ChannelPointRedeemEvent{}, false
	}
	return ChannelPointRedeemEvent{
		ID:          event.ID,
		RewardID:    event.Reward.ID,
		RewardTitle: event.Reward.Title,
		UserID:      event.UserID,
		UserLogin:   strings.ToLower(event.UserLogin),
		UserName:    event.UserName,
		UserInput:   event.UserInput,
		RedeemedAt:  redeemedAt,
	}, true
}

type flexibleBool bool

func (b *flexibleBool) UnmarshalJSON(data []byte) error {
	var asBool bool
	if err := json.Unmarshal(data, &asBool); err == nil {
		*b = flexibleBool(asBool)
		return nil
	}

	var asString string
	if err := json.Unmarshal(data, &asString); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(asString)) {
	case "true":
		*b = true
	case "false", "":
		*b = false
	default:
		return fmt.Errorf("parse bool %q", asString)
	}
	return nil
}

func eventSubHasBadge(badges []eventSubBadge, setID string) bool {
	for _, badge := range badges {
		if badge.SetID == setID {
			return true
		}
	}
	return false
}

func eventSubFragmentsEmotes(fragments []eventSubChatFragment) []Emote {
	byID := map[string]*Emote{}
	order := []string{}
	for _, fragment := range fragments {
		if fragment.Type != "emote" || fragment.Emote == nil || fragment.Emote.ID == "" {
			continue
		}
		emote := byID[fragment.Emote.ID]
		if emote == nil {
			emote = &Emote{ID: fragment.Emote.ID, Name: fragment.Text}
			byID[fragment.Emote.ID] = emote
			order = append(order, fragment.Emote.ID)
		}
		emote.Count++
	}
	if len(order) == 0 {
		return nil
	}
	emotes := make([]Emote, 0, len(order))
	for _, id := range order {
		emotes = append(emotes, *byID[id])
	}
	return emotes
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
