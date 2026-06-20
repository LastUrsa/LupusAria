package twitch

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

const twitchIRCAddr = "irc.chat.twitch.tv:6697"

type Config struct {
	Username string
	Token    string
	Channel  string
}

type Message struct {
	Channel                string
	Username               string
	DisplayName            string
	Text                   string
	Raw                    string
	Emotes                 []Emote
	ReplyParentDisplayName string
	ReplyParentUserLogin   string
	ReplyParentText        string
	IsBroadcaster          bool
	IsMod                  bool
}

type Emote struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type Client struct {
	cfg    Config
	logger *slog.Logger

	mu   sync.Mutex
	conn net.Conn
}

func NewClient(cfg Config, logger *slog.Logger) *Client {
	return &Client{cfg: cfg, logger: logger}
}

func (c *Client) Connect(ctx context.Context) (<-chan Message, error) {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 15 * time.Second}, "tcp", twitchIRCAddr, &tls.Config{MinVersion: tls.VersionTLS12})
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	if err := c.writeRaw("CAP REQ :twitch.tv/tags twitch.tv/commands"); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := c.writeRaw("PASS " + c.cfg.Token); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := c.writeRaw("NICK " + c.cfg.Username); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := c.writeRaw("JOIN #" + c.cfg.Channel); err != nil {
		_ = conn.Close()
		return nil, err
	}

	messages := make(chan Message, 64)
	go c.readLoop(ctx, conn, messages)

	c.logger.Info("connected to twitch chat", "channel", c.cfg.Channel)
	return messages, nil
}

func (c *Client) Say(channel, text string) error {
	channel = strings.TrimPrefix(channel, "#")
	return c.writeRaw(fmt.Sprintf("PRIVMSG #%s :%s", channel, text))
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) readLoop(ctx context.Context, conn net.Conn, messages chan<- Message) {
	defer close(messages)
	defer conn.Close()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		raw := scanner.Text()
		if strings.HasPrefix(raw, "PING ") {
			_ = c.writeRaw("PONG " + strings.TrimPrefix(raw, "PING "))
			continue
		}
		msg, ok := parseMessage(raw)
		if !ok {
			continue
		}
		messages <- msg
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil && err != io.EOF {
		c.logger.Warn("twitch read loop ended", "error", err)
	}
}

func (c *Client) writeRaw(line string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return io.ErrClosedPipe
	}
	line = sanitizeIRCLine(line)
	_, err := fmt.Fprintf(c.conn, "%s\r\n", line)
	return err
}

func sanitizeIRCLine(line string) string {
	line = strings.ReplaceAll(line, "\r", " ")
	line = strings.ReplaceAll(line, "\n", " ")
	return strings.Join(strings.Fields(line), " ")
}

func parseMessage(raw string) (Message, bool) {
	tags := map[string]string{}
	line := raw
	if strings.HasPrefix(line, "@") {
		rawTags, rest, ok := strings.Cut(line, " ")
		if !ok {
			return Message{}, false
		}
		line = rest
		for _, pair := range strings.Split(strings.TrimPrefix(rawTags, "@"), ";") {
			key, value, _ := strings.Cut(pair, "=")
			tags[key] = unescapeIRCTag(value)
		}
	}

	if !strings.Contains(line, " PRIVMSG ") {
		return Message{}, false
	}

	prefix, body, ok := strings.Cut(line, " PRIVMSG ")
	if !ok {
		return Message{}, false
	}
	username := ""
	if strings.HasPrefix(prefix, ":") {
		userPart := strings.TrimPrefix(prefix, ":")
		username, _, _ = strings.Cut(userPart, "!")
	}

	channelPart, text, ok := strings.Cut(body, " :")
	if !ok {
		return Message{}, false
	}

	displayName := tags["display-name"]
	if displayName == "" {
		displayName = username
	}
	isBroadcaster := hasBadge(tags["badges"], "broadcaster")
	isMod := isBroadcaster || hasBadge(tags["badges"], "moderator") || tags["mod"] == "1"

	return Message{
		Channel:                strings.TrimPrefix(channelPart, "#"),
		Username:               strings.ToLower(username),
		DisplayName:            displayName,
		Text:                   text,
		Raw:                    raw,
		Emotes:                 parseEmotesTag(tags["emotes"], text),
		ReplyParentDisplayName: tags["reply-parent-display-name"],
		ReplyParentUserLogin:   tags["reply-parent-user-login"],
		ReplyParentText:        tags["reply-parent-msg-body"],
		IsBroadcaster:          isBroadcaster,
		IsMod:                  isMod,
	}, true
}

func hasBadge(raw, badge string) bool {
	for _, item := range strings.Split(raw, ",") {
		name, _, _ := strings.Cut(item, "/")
		if name == badge {
			return true
		}
	}
	return false
}

func parseEmotesTag(raw, text string) []Emote {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	runes := []rune(text)
	byID := map[string]*Emote{}
	order := []string{}
	for _, item := range strings.Split(raw, "/") {
		id, rangesRaw, ok := strings.Cut(item, ":")
		id = strings.TrimSpace(id)
		if !ok || id == "" || rangesRaw == "" {
			continue
		}
		for _, rangeRaw := range strings.Split(rangesRaw, ",") {
			startRaw, endRaw, ok := strings.Cut(strings.TrimSpace(rangeRaw), "-")
			if !ok {
				continue
			}
			start, startOK := parseNonNegativeInt(startRaw)
			end, endOK := parseNonNegativeInt(endRaw)
			if !startOK || !endOK || start < 0 || end < start || start >= len(runes) {
				continue
			}
			if end >= len(runes) {
				end = len(runes) - 1
			}
			emote, exists := byID[id]
			if !exists {
				name := string(runes[start : end+1])
				emote = &Emote{ID: id, Name: name}
				byID[id] = emote
				order = append(order, id)
			}
			emote.Count++
		}
	}
	if len(order) == 0 {
		return nil
	}
	out := make([]Emote, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
}

func parseNonNegativeInt(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

func unescapeIRCTag(value string) string {
	if !strings.Contains(value, `\`) {
		return value
	}

	var out strings.Builder
	escaped := false
	for _, r := range value {
		if escaped {
			switch r {
			case 's':
				out.WriteByte(' ')
			case ':':
				out.WriteByte(';')
			case 'r':
				out.WriteByte('\r')
			case 'n':
				out.WriteByte('\n')
			default:
				out.WriteRune(r)
			}
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		out.WriteRune(r)
	}
	if escaped {
		out.WriteByte('\\')
	}
	return out.String()
}
