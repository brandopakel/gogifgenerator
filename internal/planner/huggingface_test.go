package planner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/intent"
)

func TestHuggingFacePlanUsesChatCompletionsStructuredOutput(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer hf-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"caption\":\"LIFT OFF\",\"palette\":[\"#111111\",\"#222222\",\"#333333\",\"#AAAAAA\",\"#FFFFFF\"],\"motion\":\"pulse\",\"subject\":\"a heavy rocket\",\"action\":\"launching\",\"setting\":\"from a desert pad\",\"camera\":\"pull-back\",\"style\":\"photoreal\",\"mood\":\"dramatic\",\"keywords\":[\"rocket\",\"launch\"]}"}}]}`))
	}))
	defer server.Close()

	result, err := (HuggingFace{APIKey: "hf-token", Model: "openai/gpt-oss-120b", BaseURL: server.URL + "/v1", Client: server.Client()}).Plan(
		context.Background(), Request{Prompt: "a rocket launching", Width: 320, Height: 320},
	)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if result.Engine != "huggingface" {
		t.Fatalf("Engine = %q", result.Engine)
	}
	if result.Spec.Caption != "LIFT OFF" || result.Spec.Motion != "pulse" {
		t.Fatalf("Spec = %#v", result.Spec)
	}
	if result.Brief.Subject != "a heavy rocket" || result.Brief.Camera != intent.CameraPullBack {
		t.Fatalf("Brief = %#v", result.Brief)
	}
	if result.Brief.Source != "huggingface" {
		t.Fatalf("Brief.Source = %q", result.Brief.Source)
	}
	if result.Brief.Negative == "" {
		t.Fatal("Brief did not receive a negative prompt")
	}
	format, ok := received["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("request did not request structured output: %#v", received)
	}
	schema, ok := format["json_schema"].(map[string]any)
	if !ok || schema["strict"] != true {
		t.Fatalf("json_schema = %#v", format["json_schema"])
	}
}

func TestHuggingFacePlanAcceptsFencedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"```json\\n{\\\"subject\\\":\\\"a fox\\\",\\\"action\\\":\\\"leaping\\\",\\\"camera\\\":\\\"pan\\\",\\\"style\\\":\\\"illustration\\\",\\\"mood\\\":\\\"joyful\\\"}\\n```\"}}]}"))
	}))
	defer server.Close()

	result, err := (HuggingFace{APIKey: "token", BaseURL: server.URL + "/v1", Client: server.Client()}).Plan(
		context.Background(), Request{Prompt: "a fox leaping"},
	)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if result.Brief.Subject != "a fox" || result.Spec.Motion != "waves" {
		t.Fatalf("Plan() = %#v", result)
	}
}

func TestHuggingFacePlanRejectsInventedVocabulary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"subject\":\"a fox\",\"camera\":\"barrel-roll\"}"}}]}`))
	}))
	defer server.Close()

	_, err := (HuggingFace{APIKey: "token", BaseURL: server.URL + "/v1", Client: server.Client()}).Plan(
		context.Background(), Request{Prompt: "a fox leaping"},
	)
	if err == nil {
		t.Fatal("Plan() accepted an unsupported camera")
	}
}

func TestHuggingFacePlanFallsBackToOfflineReadingWhenModelSaysNothing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"caption\":\"HELLO\"}"}}]}`))
	}))
	defer server.Close()

	result, err := (HuggingFace{APIKey: "token", BaseURL: server.URL + "/v1", Client: server.Client()}).Plan(
		context.Background(), Request{Prompt: "a chef flipping pancakes in a diner"},
	)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if result.Brief.Subject != "chef" || result.Brief.Action != "flipping pancakes" {
		t.Fatalf("Brief did not fall back to the offline reading: %#v", result.Brief)
	}
}

func TestHuggingFaceRejectsUnapprovedEndpoints(t *testing.T) {
	for name, planner := range map[string]HuggingFace{
		"unapproved host": {APIKey: "token", BaseURL: "https://evil.example.com/v1"},
		"plain http":      {APIKey: "token", BaseURL: "http://router.huggingface.co/v1"},
		"credentials":     {APIKey: "token", BaseURL: "https://user:pass@router.huggingface.co/v1"},
		"missing token":   {BaseURL: "https://router.huggingface.co/v1"},
	} {
		if _, err := planner.Plan(context.Background(), Request{Prompt: "hello"}); err == nil {
			t.Fatalf("Plan(%s) expected an error", name)
		}
	}
}

func TestHuggingFaceAllowsLoopbackWithoutAToken(t *testing.T) {
	// A locally served quantized model needs no vendor token.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization was sent to a local server: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"subject\":\"a cat\",\"action\":\"sleeping\"}"}}]}`))
	}))
	defer server.Close()

	result, err := (HuggingFace{BaseURL: server.URL + "/v1", Client: server.Client()}).Plan(
		context.Background(), Request{Prompt: "a cat sleeping"},
	)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if result.Brief.Subject != "a cat" {
		t.Fatalf("Brief = %#v", result.Brief)
	}
}

func TestHuggingFaceWithFallbackKeepsGenerationAvailable(t *testing.T) {
	result, err := (WithFallback{Primary: HuggingFace{}, Fallback: Local{}}).Plan(
		context.Background(), Request{Prompt: "still works"},
	)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if !strings.HasSuffix(result.Engine, "-fallback") {
		t.Fatalf("Engine = %q", result.Engine)
	}
	if result.Brief.Source != "local" {
		t.Fatalf("fallback brief = %#v", result.Brief)
	}
}
