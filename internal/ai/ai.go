package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"lupusaria/internal/config"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	Estimated    bool
}

type Response struct {
	Text  string
	Usage Usage
}

const (
	defaultMaxOutputTokens = 1024
	defaultMaxRetries      = 3
)

type Client interface {
	Complete(ctx context.Context, messages []Message) (Response, error)
}

func NewClient(cfg config.AIConfig) (Client, error) {
	primaryCfg := cfg
	primaryCfg.Fallback = nil
	primary, err := newSingleClient(primaryCfg)
	if err != nil {
		return nil, err
	}
	if cfg.Fallback == nil {
		return primary, nil
	}
	fallbackCfg := *cfg.Fallback
	fallbackCfg.Fallback = nil
	fallback, err := newSingleClient(fallbackCfg)
	if err != nil {
		return nil, fmt.Errorf("initialize fallback AI provider: %w", err)
	}
	return fallbackClient{primary: primary, fallback: fallback}, nil
}

func newSingleClient(cfg config.AIConfig) (Client, error) {
	switch cfg.Provider {
	case "", "mock":
		return MockClient{}, nil
	case "gemini":
		return NewGeminiClient(cfg), nil
	case "openai-compatible":
		return NewOpenAICompatibleClient(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported AI_PROVIDER %q", cfg.Provider)
	}
}

type fallbackClient struct {
	primary  Client
	fallback Client
}

func (c fallbackClient) Complete(ctx context.Context, messages []Message) (Response, error) {
	response, err := c.primary.Complete(ctx, messages)
	if err == nil {
		return response, nil
	}
	fallbackResponse, fallbackErr := c.fallback.Complete(ctx, messages)
	if fallbackErr == nil {
		return fallbackResponse, nil
	}
	return Response{}, fmt.Errorf("primary AI failed: %v; fallback AI failed: %w", err, fallbackErr)
}

type GeminiClient struct {
	apiKey        string
	model         string
	thinkingLevel string
	maxTokens     int
	maxRetries    int
	inputPrice    float64
	outputPrice   float64
	httpClient    *http.Client
}

func NewGeminiClient(cfg config.AIConfig) *GeminiClient {
	return &GeminiClient{
		apiKey:        cfg.APIKey,
		model:         cfg.Model,
		thinkingLevel: strings.TrimSpace(cfg.GeminiThinkingLevel),
		maxTokens:     positiveOrDefault(cfg.MaxOutputTokens, defaultMaxOutputTokens),
		maxRetries:    nonNegativeOrDefault(cfg.MaxRetries, defaultMaxRetries),
		inputPrice:    cfg.InputPricePerMillion,
		outputPrice:   cfg.OutputPricePerMillion,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

func (c *GeminiClient) Complete(ctx context.Context, messages []Message) (Response, error) {
	var lastErr error
	attempts := c.maxRetries + 1
	for attempt := 0; attempt < attempts; attempt++ {
		response, err := c.completeOnce(ctx, messages)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if !isRetryableError(err) || attempt == attempts-1 {
			break
		}
		if err := sleepBeforeRetry(ctx, attempt); err != nil {
			return Response{}, err
		}
	}
	return Response{}, lastErr
}

func (c *GeminiClient) completeOnce(ctx context.Context, messages []Message) (Response, error) {
	systemInstruction, prompt := splitSystemAndUserPrompt(messages)
	payload := geminiGenerateRequest{
		Contents: []geminiContent{
			{Role: "user", Parts: []geminiPart{{Text: prompt}}},
		},
		GenerationConfig: geminiGenerationConfig{
			MaxOutputTokens: c.maxTokens,
		},
	}
	if geminiSupportsThinkingLevel(c.model) && c.thinkingLevel != "" {
		payload.GenerationConfig.ThinkingConfig = &geminiThinkingConfig{ThinkingLevel: c.thinkingLevel}
	}
	if systemInstruction != "" {
		payload.SystemInstruction = &geminiSystemInstruction{
			Parts: []geminiPart{{Text: systemInstruction}},
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}

	endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", url.PathEscape(c.model))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, retryableError{err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr geminiErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Error.Message != "" {
			return Response{}, apiStatusError{status: resp.StatusCode, message: apiErr.Error.Message}
		}
		return Response{}, apiStatusError{status: resp.StatusCode, message: fmt.Sprintf("gemini request failed with status %s", resp.Status)}
	}

	var result geminiGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Response{}, err
	}

	text := result.Text()
	if strings.TrimSpace(text) == "" {
		finishReason := result.FinishReason()
		if finishReason == "MAX_TOKENS" {
			return Response{}, retryableError{errors.New("gemini response stopped because it reached max output tokens without text")}
		}
		if finishReason == "SAFETY" {
			return Response{}, errors.New("gemini response blocked by safety filters")
		}
		return Response{}, retryableError{errors.New("gemini response did not include text")}
	}

	usage := Usage{
		InputTokens:  result.UsageMetadata.PromptTokenCount,
		OutputTokens: result.UsageMetadata.CandidatesTokenCount,
	}
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		usage.CostUSD = estimateCostUSD(usage.InputTokens, usage.OutputTokens, c.inputPrice, c.outputPrice)
	}

	return Response{Text: strings.TrimSpace(text), Usage: usage}, nil
}

type MockClient struct{}

func (MockClient) Complete(_ context.Context, messages []Message) (Response, error) {
	last := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			last = messages[i].Content
			break
		}
	}
	if last == "" {
		return Response{Text: "I am awake, but I need something to answer."}, nil
	}
	return Response{Text: "Mock reply: I heard you. Configure AI_PROVIDER=openai-compatible when you want real model responses."}, nil
}

type OpenAICompatibleClient struct {
	apiKey      string
	baseURL     string
	model       string
	maxTokens   int
	maxRetries  int
	inputPrice  float64
	outputPrice float64
	httpClient  *http.Client
}

func NewOpenAICompatibleClient(cfg config.AIConfig) *OpenAICompatibleClient {
	return &OpenAICompatibleClient{
		apiKey:      cfg.APIKey,
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		model:       cfg.Model,
		maxTokens:   positiveOrDefault(cfg.MaxOutputTokens, defaultMaxOutputTokens),
		maxRetries:  nonNegativeOrDefault(cfg.MaxRetries, defaultMaxRetries),
		inputPrice:  cfg.InputPricePerMillion,
		outputPrice: cfg.OutputPricePerMillion,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

func (c *OpenAICompatibleClient) Complete(ctx context.Context, messages []Message) (Response, error) {
	var lastErr error
	attempts := c.maxRetries + 1
	for attempt := 0; attempt < attempts; attempt++ {
		response, err := c.completeOnce(ctx, messages)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if !isRetryableError(err) || attempt == attempts-1 {
			break
		}
		if err := sleepBeforeRetry(ctx, attempt); err != nil {
			return Response{}, err
		}
	}
	return Response{}, lastErr
}

func (c *OpenAICompatibleClient) completeOnce(ctx context.Context, messages []Message) (Response, error) {
	payload := chatCompletionRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: 0.7,
		MaxTokens:   c.maxTokens,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, retryableError{err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr apiErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Error.Message != "" {
			return Response{}, apiStatusError{status: resp.StatusCode, message: apiErr.Error.Message}
		}
		return Response{}, apiStatusError{status: resp.StatusCode, message: fmt.Sprintf("ai request failed with status %s", resp.Status)}
	}

	var result chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Response{}, err
	}
	if len(result.Choices) == 0 {
		return Response{}, errors.New("ai response did not include choices")
	}
	usage := Usage{
		InputTokens:  result.Usage.PromptTokens,
		OutputTokens: result.Usage.CompletionTokens,
	}
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		usage.CostUSD = estimateCostUSD(usage.InputTokens, usage.OutputTokens, c.inputPrice, c.outputPrice)
	}
	text := strings.TrimSpace(result.Choices[0].Message.Content)
	if text == "" {
		return Response{}, retryableError{errors.New("ai response did not include text")}
	}
	return Response{
		Text:  text,
		Usage: usage,
	}, nil
}

type chatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type apiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

type geminiGenerateRequest struct {
	SystemInstruction *geminiSystemInstruction `json:"system_instruction,omitempty"`
	Contents          []geminiContent          `json:"contents"`
	GenerationConfig  geminiGenerationConfig   `json:"generationConfig,omitempty"`
}

type geminiSystemInstruction struct {
	Parts []geminiPart `json:"parts"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int                   `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

type geminiThinkingConfig struct {
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
}

type geminiGenerateResponse struct {
	Candidates []struct {
		Content      geminiContent `json:"content"`
		FinishReason string        `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func (r geminiGenerateResponse) Text() string {
	if len(r.Candidates) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, part := range r.Candidates[0].Content.Parts {
		builder.WriteString(part.Text)
	}
	return builder.String()
}

func (r geminiGenerateResponse) FinishReason() string {
	if len(r.Candidates) == 0 {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(r.Candidates[0].FinishReason))
}

type geminiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func estimateCostUSD(inputTokens, outputTokens int, inputPricePerMillion, outputPricePerMillion float64) float64 {
	inputCost := (float64(inputTokens) / 1_000_000) * inputPricePerMillion
	outputCost := (float64(outputTokens) / 1_000_000) * outputPricePerMillion
	return inputCost + outputCost
}

func geminiSupportsThinkingLevel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gemini-3")
}

type retryableError struct {
	err error
}

func (e retryableError) Error() string {
	return e.err.Error()
}

func (e retryableError) Unwrap() error {
	return e.err
}

type apiStatusError struct {
	status  int
	message string
}

func (e apiStatusError) Error() string {
	return e.message
}

func isRetryableError(err error) bool {
	var retryable retryableError
	if errors.As(err, &retryable) {
		return true
	}
	var statusErr apiStatusError
	if errors.As(err, &statusErr) {
		return statusErr.status == http.StatusTooManyRequests ||
			statusErr.status == http.StatusInternalServerError ||
			statusErr.status == http.StatusBadGateway ||
			statusErr.status == http.StatusServiceUnavailable ||
			statusErr.status == http.StatusGatewayTimeout
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "timeout") ||
		strings.Contains(message, "timed out") ||
		strings.Contains(message, "connection reset") ||
		strings.Contains(message, "temporary") ||
		strings.Contains(message, "network")
}

func sleepBeforeRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(500*(1<<attempt)) * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func positiveOrDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func nonNegativeOrDefault(value, fallback int) int {
	if value >= 0 {
		return value
	}
	return fallback
}

func splitSystemAndUserPrompt(messages []Message) (string, string) {
	var system strings.Builder
	var user strings.Builder
	for _, message := range messages {
		if message.Role == "system" {
			system.WriteString(message.Content)
			system.WriteByte('\n')
			continue
		}
		user.WriteString(strings.ToUpper(message.Role))
		user.WriteString(": ")
		user.WriteString(message.Content)
		user.WriteByte('\n')
	}
	return strings.TrimSpace(system.String()), strings.TrimSpace(user.String())
}
