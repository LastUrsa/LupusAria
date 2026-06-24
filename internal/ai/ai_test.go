package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"lupusaria/internal/config"
)

type staticClient struct {
	response Response
	err      error
}

func (c staticClient) Complete(context.Context, []Message) (Response, error) {
	return c.response, c.err
}

func TestFallbackClientUsesFallbackWhenPrimaryFails(t *testing.T) {
	client := fallbackClient{
		primary:  staticClient{err: errors.New("primary down")},
		fallback: staticClient{response: Response{Text: "backup online"}},
	}

	response, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "backup online" {
		t.Fatalf("response = %q, want backup online", response.Text)
	}
}

func TestOpenAICompatibleCompleteParsesResponseAndUsage(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		if r.URL.Scheme != "https" || r.URL.Host != "ai.test" {
			t.Fatalf("url = %s", r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}

		var body chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "test-model" {
			t.Fatalf("model = %q, want test-model", body.Model)
		}
		if body.MaxTokens != 99 {
			t.Fatalf("max tokens = %d, want 99", body.MaxTokens)
		}

		return jsonResponse(http.StatusOK, `{
			"choices": [{"message": {"role": "assistant", "content": "  hello from ai  "}}],
			"usage": {"prompt_tokens": 1000, "completion_tokens": 2000}
		}`), nil
	})

	client := NewOpenAICompatibleClient(config.AIConfig{
		APIKey:                "test-key",
		BaseURL:               "https://ai.test",
		Model:                 "test-model",
		MaxOutputTokens:       99,
		InputPricePerMillion:  1,
		OutputPricePerMillion: 2,
	})
	client.httpClient = &http.Client{Transport: transport}

	response, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "hello from ai" {
		t.Fatalf("text = %q", response.Text)
	}
	if response.Usage.InputTokens != 1000 || response.Usage.OutputTokens != 2000 {
		t.Fatalf("usage = %#v", response.Usage)
	}
	if response.Usage.CostUSD != 0.005 {
		t.Fatalf("cost = %f, want 0.005", response.Usage.CostUSD)
	}
}

func TestOpenAICompatibleCompleteReturnsAPIErrorMessage(t *testing.T) {
	client := NewOpenAICompatibleClient(config.AIConfig{BaseURL: "https://ai.test", Model: "test-model"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusTooManyRequests, `{"error":{"message":"rate limited"}}`), nil
	})}

	_, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hello"}})
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("err = %v, want rate limited", err)
	}
}

func TestOpenAICompatibleCompleteRequiresChoice(t *testing.T) {
	client := NewOpenAICompatibleClient(config.AIConfig{BaseURL: "https://ai.test", Model: "test-model"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"choices":[]}`), nil
	})}

	_, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hello"}})
	if err == nil || !strings.Contains(err.Error(), "did not include choices") {
		t.Fatalf("err = %v, want missing choices", err)
	}
}

func TestOpenAICompatibleCompleteRequiresText(t *testing.T) {
	client := NewOpenAICompatibleClient(config.AIConfig{BaseURL: "https://ai.test", Model: "test-model"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"   "}}]}`), nil
	})}

	_, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hello"}})
	if err == nil || !strings.Contains(err.Error(), "did not include text") {
		t.Fatalf("err = %v, want missing text", err)
	}
}

func TestGeminiCompleteUsesSystemInstructionAndParsesResponse(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", req.Method)
		}
		if got := req.Header.Get("x-goog-api-key"); got != "gemini-key" {
			t.Fatalf("x-goog-api-key = %q", got)
		}

		var body geminiGenerateRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.SystemInstruction == nil || len(body.SystemInstruction.Parts) != 1 || body.SystemInstruction.Parts[0].Text != "system text" {
			t.Fatalf("system instruction = %#v", body.SystemInstruction)
		}
		if len(body.Contents) != 1 || !strings.Contains(body.Contents[0].Parts[0].Text, "user text") {
			t.Fatalf("contents = %#v", body.Contents)
		}
		if body.GenerationConfig.MaxOutputTokens != 123 {
			t.Fatalf("max output tokens = %d, want 123", body.GenerationConfig.MaxOutputTokens)
		}
		if body.GenerationConfig.ThinkingConfig == nil || body.GenerationConfig.ThinkingConfig.ThinkingLevel != "high" {
			t.Fatalf("thinking config = %#v", body.GenerationConfig.ThinkingConfig)
		}

		return jsonResponse(http.StatusOK, `{
			"candidates": [{"content": {"parts": [{"text": " gemini says hi "} ]}}],
			"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 20}
		}`), nil
	})
	client := NewGeminiClient(config.AIConfig{
		APIKey:                "gemini-key",
		Model:                 "gemini-3.1-flash-lite",
		MaxOutputTokens:       123,
		GeminiThinkingLevel:   "high",
		InputPricePerMillion:  1,
		OutputPricePerMillion: 2,
	})
	client.httpClient = &http.Client{Transport: transport}

	response, err := client.Complete(context.Background(), []Message{
		{Role: "system", Content: "system text"},
		{Role: "user", Content: "user text"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "gemini says hi" {
		t.Fatalf("text = %q", response.Text)
	}
	if response.Usage.InputTokens != 10 || response.Usage.OutputTokens != 20 {
		t.Fatalf("usage = %#v", response.Usage)
	}
}

func TestGeminiSearchEnablesGoogleSearchTool(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body geminiGenerateRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Tools) != 1 || body.Tools[0].GoogleSearch == nil {
			t.Fatalf("tools = %#v, want googleSearch", body.Tools)
		}
		if body.SystemInstruction == nil || !strings.Contains(body.SystemInstruction.Parts[0].Text, "Google Search grounding") {
			t.Fatalf("system instruction = %#v", body.SystemInstruction)
		}

		return jsonResponse(http.StatusOK, `{
			"candidates": [{"content": {"parts": [{"text": " grounded answer "} ]}}],
			"usageMetadata": {"promptTokenCount": 11, "candidatesTokenCount": 22}
		}`), nil
	})
	client := NewGeminiClient(config.AIConfig{APIKey: "gemini-key", Model: "gemini-3.1-flash-lite"})
	client.httpClient = &http.Client{Transport: transport}

	response, err := client.Search(context.Background(), []Message{{Role: "user", Content: "current game question"}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "grounded answer" {
		t.Fatalf("text = %q", response.Text)
	}
}

func TestGeminiAnalyzeImageSendsInlineImageData(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body geminiGenerateRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		parts := body.Contents[0].Parts
		if len(parts) != 2 || parts[1].InlineData == nil {
			t.Fatalf("parts = %#v, want inline image data", parts)
		}
		if parts[1].InlineData.MIMEType != "image/png" || parts[1].InlineData.Data == "" {
			t.Fatalf("inline data = %#v", parts[1].InlineData)
		}

		return jsonResponse(http.StatusOK, `{
			"candidates": [{"content": {"parts": [{"text": " snapshot description "} ]}}],
			"usageMetadata": {"promptTokenCount": 12, "candidatesTokenCount": 23}
		}`), nil
	})
	client := NewGeminiClient(config.AIConfig{APIKey: "gemini-key", Model: "gemini-3.1-flash-lite"})
	client.httpClient = &http.Client{Transport: transport}

	response, err := client.AnalyzeImage(context.Background(), "describe", []byte("png"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "snapshot description" {
		t.Fatalf("text = %q", response.Text)
	}
}

func TestGeminiCompleteOmitsThinkingConfigForUnsupportedModels(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body geminiGenerateRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.GenerationConfig.ThinkingConfig != nil {
			t.Fatalf("thinking config should be omitted, got %#v", body.GenerationConfig.ThinkingConfig)
		}
		return jsonResponse(http.StatusOK, `{
			"candidates": [{"content": {"parts": [{"text": "lite says hi"}]}}],
			"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 20}
		}`), nil
	})
	client := NewGeminiClient(config.AIConfig{APIKey: "gemini-key", Model: "gemini-2.5-flash-lite"})
	client.httpClient = &http.Client{Transport: transport}

	if _, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hello"}}); err != nil {
		t.Fatal(err)
	}
}

func TestGeminiCompleteReturnsAPIErrorMessage(t *testing.T) {
	client := NewGeminiClient(config.AIConfig{APIKey: "gemini-key", Model: "gemini-test"})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusBadRequest, `{"error":{"message":"bad prompt"}}`), nil
	})}

	_, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hello"}})
	if err == nil || !strings.Contains(err.Error(), "bad prompt") {
		t.Fatalf("err = %v, want bad prompt", err)
	}
}

func TestGeminiCompleteRetriesEmptyText(t *testing.T) {
	attempts := 0
	client := NewGeminiClient(config.AIConfig{
		APIKey:              "gemini-key",
		Model:               "gemini-test",
		MaxRetries:          1,
		GeminiThinkingLevel: "high",
	})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return jsonResponse(http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"   "}]} }]}`), nil
		}
		return jsonResponse(http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"retry worked"}]}}]}`), nil
	})}

	response, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if response.Text != "retry worked" {
		t.Fatalf("text = %q", response.Text)
	}
}

func TestGeminiCompleteRetriesTextWithMaxTokenFinish(t *testing.T) {
	attempts := 0
	client := NewGeminiClient(config.AIConfig{APIKey: "gemini-key", Model: "gemini-test", MaxRetries: 1})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return jsonResponse(http.StatusOK, `{
				"candidates": [{"finishReason":"MAX_TOKENS","content": {"parts": [{"text": "partial"}]}}]
			}`), nil
		}
		return jsonResponse(http.StatusOK, `{
			"candidates": [{"finishReason":"STOP","content": {"parts": [{"text": "complete"}]}}]
		}`), nil
	})}

	response, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if response.Text != "complete" {
		t.Fatalf("text = %q, want complete", response.Text)
	}
}

func TestGeminiCompleteRetriesEmptyMaxTokenFinish(t *testing.T) {
	attempts := 0
	client := NewGeminiClient(config.AIConfig{APIKey: "gemini-key", Model: "gemini-test", MaxRetries: 1})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return jsonResponse(http.StatusOK, `{"candidates": [{"finishReason":"MAX_TOKENS","content": {"parts": []}}]}`), nil
		}
		return jsonResponse(http.StatusOK, `{"candidates": [{"content": {"parts": [{"text":"retry after max"}]}}]}`), nil
	})}

	response, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "retry after max" {
		t.Fatalf("text = %q", response.Text)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
