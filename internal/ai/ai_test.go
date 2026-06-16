package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"lupusaria/internal/config"
)

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
		if body.MaxTokens != maxOutputTokens {
			t.Fatalf("max tokens = %d, want %d", body.MaxTokens, maxOutputTokens)
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

		return jsonResponse(http.StatusOK, `{
			"candidates": [{"content": {"parts": [{"text": " gemini says hi "} ]}}],
			"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 20}
		}`), nil
	})
	client := NewGeminiClient(config.AIConfig{
		APIKey:                "gemini-key",
		Model:                 "gemini-test",
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
