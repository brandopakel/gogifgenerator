package planner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIPlanUsesResponsesStructuredOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		text, ok := request["text"].(map[string]any)
		if !ok {
			t.Fatal("request is missing text config")
		}
		format, ok := text["format"].(map[string]any)
		if !ok || format["type"] != "json_schema" {
			t.Fatalf("format = %#v", format)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
          "output": [{
            "type": "message",
            "content": [{
              "type": "output_text",
              "text": "{\"caption\":\"SHIP IT\",\"palette\":[\"#111111\",\"#222222\",\"#333333\",\"#AAAAAA\",\"#FFFFFF\"],\"motion\":\"pulse\"}"
            }]
          }]
        }`))
	}))
	defer server.Close()

	result, err := (OpenAI{APIKey: "test-key", Model: "test-model", BaseURL: server.URL, Client: server.Client()}).Plan(
		context.Background(),
		Request{Prompt: "we shipped", Width: 320, Height: 320},
	)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if result.Engine != "openai" || result.Spec.Caption != "SHIP IT" || result.Spec.Motion != "pulse" {
		t.Fatalf("Plan() = %#v", result)
	}
}

func TestWithFallback(t *testing.T) {
	result, err := (WithFallback{
		Primary:  OpenAI{},
		Fallback: Local{},
	}).Plan(context.Background(), Request{Prompt: "still works"})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if result.Engine != "local-fallback" {
		t.Fatalf("engine = %q", result.Engine)
	}
}
