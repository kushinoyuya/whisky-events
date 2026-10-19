package llm

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
)

// roundTripFunc stubs the transport so tests need no listening socket.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stubResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       ioutil.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestGenerateSendsJSONRequest(t *testing.T) {
	var got generateRequest
	o := NewOllama("http://ollama.test/", "qwen2.5:3b")
	o.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "http://ollama.test/api/generate" {
			t.Errorf("url = %s", r.URL)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return stubResponse(200, `{"response":"{\"events\":[]}","done":true}`), nil
	})

	out, err := o.Generate(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if out != `{"events":[]}` {
		t.Errorf("response = %q", out)
	}
	if got.Model != "qwen2.5:3b" || got.Format != "json" || got.Stream {
		t.Errorf("request = %+v", got)
	}
	if got.Options["temperature"].(float64) != 0 {
		t.Errorf("temperature = %v", got.Options["temperature"])
	}
}

func TestGenerateReportsServerError(t *testing.T) {
	o := NewOllama("http://ollama.test", "missing")
	o.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return stubResponse(404, `{"error":"model 'missing' not found"}`), nil
	})

	_, err := o.Generate(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}
