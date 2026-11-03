package aiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestClientUsesOneOpenAICompatibleProtocolForConfiguredEndpoints(t *testing.T) {
	for _, name := range []string{"DeepSeek", "ChatGPT", "Ollama"} {
		name := name
		t.Run(name, func(t *testing.T) {
			server := openAIStub(t, func(http.ResponseWriter, *http.Request) response {
				return response{content: name + " reply"}
			})
			defer server.Close()

			client := New(Config{Endpoints: []Endpoint{{Name: name, BaseURL: server.URL, Model: "test-model"}}})
			result, err := client.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hello"}}, Idempotent: true})
			if err != nil {
				t.Fatal(err)
			}
			if result.Content != name+" reply" || result.Endpoint != name {
				t.Fatalf("result = %#v", result)
			}
			t.Logf("endpoint=%s content=%q", result.Endpoint, result.Content)
		})
	}
}

func TestClientTimesOutThenUsesNextEndpoint(t *testing.T) {
	var attempts []string
	var mu sync.Mutex
	slow := openAIStub(t, func(http.ResponseWriter, *http.Request) response {
		mu.Lock()
		attempts = append(attempts, "Ollama")
		mu.Unlock()
		time.Sleep(150 * time.Millisecond)
		return response{content: "too late"}
	})
	defer slow.Close()
	fast := openAIStub(t, func(http.ResponseWriter, *http.Request) response {
		mu.Lock()
		attempts = append(attempts, "ChatGPT")
		mu.Unlock()
		return response{content: "fallback reply"}
	})
	defer fast.Close()

	client := New(Config{Endpoints: []Endpoint{{Name: "Ollama", BaseURL: slow.URL}, {Name: "ChatGPT", BaseURL: fast.URL}}, Timeout: 30 * time.Millisecond})
	started := time.Now()
	result, err := client.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hello"}}, Idempotent: true})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if result.Endpoint != "ChatGPT" || result.Content != "fallback reply" {
		t.Fatalf("result = %#v", result)
	}
	if fmt.Sprint(attempts) != "[Ollama ChatGPT]" {
		t.Fatalf("attempts = %v", attempts)
	}
	if elapsed >= 120*time.Millisecond {
		t.Fatalf("elapsed = %s, timeout was not enforced", elapsed)
	}
	t.Logf("attempts=%v elapsed=%s switch_reason=timeout", attempts, elapsed)
}

func TestClientFallsBackToLocalRuleAfterAllEndpointsFail(t *testing.T) {
	failing := openAIStub(t, func(http.ResponseWriter, *http.Request) response {
		return response{status: http.StatusServiceUnavailable}
	})
	defer failing.Close()
	client := New(Config{
		Endpoints: []Endpoint{{Name: "DeepSeek", BaseURL: failing.URL}, {Name: "ChatGPT", BaseURL: failing.URL}, {Name: "Ollama", BaseURL: failing.URL}},
		Fallback: func(Request) Result {
			return Result{Content: "local rule response", Endpoint: "local-rule", Fallback: true}
		},
	})

	result, err := client.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hello"}}, Idempotent: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "local rule response" || !result.Fallback {
		t.Fatalf("result = %#v", result)
	}
	t.Logf("endpoint=%s content=%q", result.Endpoint, result.Content)
}

func TestClientRetryLimitsAndDoesNotRepeatNonIdempotentRequest(t *testing.T) {
	for _, tc := range []struct {
		name       string
		idempotent bool
		maxRetries int
		wantCalls  int
	}{
		{name: "idempotent_retries_are_bounded", idempotent: true, maxRetries: 2, wantCalls: 3},
		{name: "non_idempotent_is_not_repeated", idempotent: false, maxRetries: 2, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := openAIStub(t, func(http.ResponseWriter, *http.Request) response {
				calls++
				return response{status: http.StatusBadGateway}
			})
			defer server.Close()
			client := New(Config{
				Endpoints:  []Endpoint{{Name: "test", BaseURL: server.URL}},
				MaxRetries: tc.maxRetries,
				RetryDelay: time.Millisecond,
				Fallback:   func(Request) Result { return Result{Content: "local", Fallback: true} },
			})
			result, err := client.Complete(context.Background(), Request{Idempotent: tc.idempotent})
			if err != nil || !result.Fallback {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if calls != tc.wantCalls {
				t.Fatalf("calls=%d want=%d", calls, tc.wantCalls)
			}
			t.Logf("idempotent=%t max_retries=%d actual_attempts=%d", tc.idempotent, tc.maxRetries, calls)
		})
	}
}

type response struct {
	status  int
	content string
}

func openAIStub(t *testing.T, fn func(http.ResponseWriter, *http.Request) response) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		response := fn(w, r)
		if response.status != 0 {
			w.WriteHeader(response.status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": response.content}}}})
	}))
}
