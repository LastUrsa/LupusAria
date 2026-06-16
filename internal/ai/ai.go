package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type Client interface {
	Complete(ctx context.Context, messages []Message) (Response, error)
}

func NewClient(cfg config.AIConfig) (Client, error) {
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

type GeminiClient struct {
	apiKey      string
	model       string
	inputPrice  float64
	outputPrice float64
	httpClient  *http.Client
}

func NewGeminiClient(cfg config.AIConfig) *GeminiClient {
	return &GeminiClient{
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		inputPrice:  cfg.InputPricePerMillion,
		outputPrice: cfg.OutputPricePerMillion,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

func (c *GeminiClient) Complete(ctx context.Context, messages []Message) (Response, error) {
	systemInstruction, prompt := splitSystemAndUserPrompt(messages)
	payload := geminiGenerateRequest{
		Contents: []geminiContent{
			{Role: "user", Parts: []geminiPart{{Text: prompt}}},
		},
		GenerationConfig: geminiGenerationConfig{
			MaxOutputTokens: 180,
			ThinkingConfig:  geminiThinkingConfig{ThinkingLevel: "low"},
		},
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
		return Response{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr geminiErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Error.Message != "" {
			return Response{}, errors.New(apiErr.Error.Message)
		}
		return Response{}, fmt.Errorf("gemini request failed with status %s", resp.Status)
	}

	var result geminiGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Response{}, err
	}

	text := result.Text()
	if strings.TrimSpace(text) == "" {
		return Response{}, errors.New("gemini response did not include text")
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
	return Response{Text: "Mock reply: I heard you. Configure AI_PROVIDER=gemini when you want real model responses."}, nil
}

type OpenAICompatibleClient struct {
	apiKey      string
	baseURL     string
	model       string
	inputPrice  float64
	outputPrice float64
	httpClient  *http.Client
}

func NewOpenAICompatibleClient(cfg config.AIConfig) *OpenAICompatibleClient {
	return &OpenAICompatibleClient{
		apiKey:      cfg.APIKey,
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		model:       cfg.Model,
		inputPrice:  cfg.InputPricePerMillion,
		outputPrice: cfg.OutputPricePerMillion,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

func (c *OpenAICompatibleClient) Complete(ctx context.Context, messages []Message) (Response, error) {
	payload := chatCompletionRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: 0.7,
		MaxTokens:   180,
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
		return Response{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr apiErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Error.Message != "" {
			return Response{}, errors.New(apiErr.Error.Message)
		}
		return Response{}, fmt.Errorf("ai request failed with status %s", resp.Status)
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
	return Response{
		Text:  strings.TrimSpace(result.Choices[0].Message.Content),
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
	MaxOutputTokens int                  `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  geminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

type geminiThinkingConfig struct {
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
}

type geminiGenerateResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
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
