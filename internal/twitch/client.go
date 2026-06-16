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
	Channel     string
	Username    string
	DisplayName string
	Text        string
	Raw         string
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
	_, err := fmt.Fprintf(c.conn, "%s\r\n", line)
	return err
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
			tags[key] = value
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

	return Message{
		Channel:     strings.TrimPrefix(channelPart, "#"),
		Username:    strings.ToLower(username),
		DisplayName: displayName,
		Text:        text,
		Raw:         raw,
	}, true
}
