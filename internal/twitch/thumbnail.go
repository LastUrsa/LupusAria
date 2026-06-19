package twitch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const streamThumbnailBaseURL = "https://static-cdn.jtvnw.net/previews-ttv/live_user_%s-1280x720.jpg"

type ThumbnailFetcher struct {
	httpClient *http.Client
}

func NewThumbnailFetcher() *ThumbnailFetcher {
	return &ThumbnailFetcher{
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (f *ThumbnailFetcher) FetchStreamThumbnail(ctx context.Context, channel string) ([]byte, string, error) {
	channel = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(channel, "#")))
	if channel == "" {
		return nil, "", fmt.Errorf("channel is required")
	}
	if f == nil {
		f = NewThumbnailFetcher()
	}
	if f.httpClient == nil {
		f.httpClient = &http.Client{Timeout: 5 * time.Second}
	}

	endpoint := fmt.Sprintf(streamThumbnailBaseURL, url.PathEscape(channel))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	values := req.URL.Query()
	values.Set("t", fmt.Sprintf("%d", time.Now().UnixNano()))
	req.URL.RawQuery = values.Encode()

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch stream thumbnail: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, "", err
	}
	if len(body) == 0 {
		return nil, "", fmt.Errorf("stream thumbnail was empty")
	}
	mimeType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	return body, mimeType, nil
}
