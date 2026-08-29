package clientollama_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	clienthttp "github.com/omcrgnt/client-http"
	clientollama "github.com/omcrgnt/client-ollama"
	commonv1 "github.com/omcrgnt/proto/gen/go/common/v1"
	httpv1 "github.com/omcrgnt/proto/gen/go/http/v1"
	"github.com/prometheus/client_golang/prometheus"
)

// fakeOllama mimics enough of the real Ollama HTTP surface for tests:
// HEAD / (Heartbeat), POST /api/embed, POST /api/generate (NDJSON, one line).
type fakeOllama struct {
	embedResp      *embedResponse
	embedStatus    int
	generateLine   string // raw NDJSON line; empty body if ""
	generateLine2  string // optional second NDJSON line, written after generateLine
	generateStatus int
	heartbeatErr   bool
}

type embedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
}

func (f *fakeOllama) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodHead && r.URL.Path == "/":
			if f.heartbeatErr {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/embed":
			status := f.embedStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			if f.embedResp != nil {
				_ = json.NewEncoder(w).Encode(f.embedResp)
			}
		case r.URL.Path == "/api/generate":
			w.Header().Set("Content-Type", "application/x-ndjson")
			status := f.generateStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			if f.generateLine != "" {
				fmt.Fprintln(w, f.generateLine)
			}
			if f.generateLine2 != "" {
				fmt.Fprintln(w, f.generateLine2)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func newWiredClient(t *testing.T, srv *httptest.Server) *clientollama.Client {
	t.Helper()
	reg := prometheus.NewRegistry()
	metrics := &clienthttp.HTTPClientMetrics{}
	if err := metrics.RegisterMetrics(reg); err != nil {
		t.Fatal(err)
	}
	httpBuilt, err := (&clienthttp.Config{
		Label:   commonv1.Label{Value: "ollama"},
		BaseURL: httpv1.URL{Value: srv.URL},
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	httpClient := httpBuilt.(*clienthttp.Client)
	httpClient.Inject([]any{metrics})
	t.Cleanup(func() { _ = httpClient.Close(context.Background()) })

	c := &clientollama.Client{}
	c.Inject([]any{httpClient})
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClient_NewResource(t *testing.T) {
	res, err := (&clientollama.Client{}).NewResource()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.(*clientollama.Client); !ok {
		t.Fatalf("NewResource() = %T, want *clientollama.Client", res)
	}
}

func TestClient_Deps(t *testing.T) {
	deps := (&clientollama.Client{}).Deps()
	if len(deps) != 1 {
		t.Fatalf("Deps() len = %d, want 1", len(deps))
	}
	if got, want := reflect.TypeOf(deps[0]), reflect.TypeOf((*clienthttp.Client)(nil)); got != want {
		t.Fatalf("Deps()[0] type = %v, want %v", got, want)
	}
}

func TestClient_Embed(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := &fakeOllama{embedResp: &embedResponse{Model: "m", Embeddings: [][]float32{{0.1, 0.2, 0.3}}}}
		srv := httptest.NewServer(f.handler())
		t.Cleanup(srv.Close)
		c := newWiredClient(t, srv)

		vec, err := c.Embed(t.Context(), "m", "hello")
		if err != nil {
			t.Fatal(err)
		}
		if len(vec) != 3 || vec[0] != 0.1 {
			t.Fatalf("vec = %v", vec)
		}
	})

	t.Run("empty embeddings response", func(t *testing.T) {
		f := &fakeOllama{embedResp: &embedResponse{Model: "m", Embeddings: nil}}
		srv := httptest.NewServer(f.handler())
		t.Cleanup(srv.Close)
		c := newWiredClient(t, srv)

		if _, err := c.Embed(t.Context(), "m", "hello"); err == nil {
			t.Fatal("expected an error for empty embeddings response")
		}
	})

	t.Run("upstream error", func(t *testing.T) {
		f := &fakeOllama{embedStatus: http.StatusInternalServerError}
		srv := httptest.NewServer(f.handler())
		t.Cleanup(srv.Close)
		c := newWiredClient(t, srv)

		if _, err := c.Embed(t.Context(), "m", "hello"); err == nil {
			t.Fatal("expected an error when Ollama returns a non-2xx status")
		}
	})
}

func TestClient_Generate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		line, err := json.Marshal(map[string]any{"model": "m", "response": "hi there", "done": true})
		if err != nil {
			t.Fatal(err)
		}
		f := &fakeOllama{generateLine: string(line)}
		srv := httptest.NewServer(f.handler())
		t.Cleanup(srv.Close)
		c := newWiredClient(t, srv)

		out, err := c.Generate(t.Context(), "m", "hello", nil)
		if err != nil {
			t.Fatal(err)
		}
		if out != "hi there" {
			t.Fatalf("out = %q", out)
		}
	})

	t.Run("no response received", func(t *testing.T) {
		f := &fakeOllama{generateLine: ""} // 200 OK, empty NDJSON body
		srv := httptest.NewServer(f.handler())
		t.Cleanup(srv.Close)
		c := newWiredClient(t, srv)

		if _, err := c.Generate(t.Context(), "m", "hello", nil); err == nil {
			t.Fatal("expected an error when the server sends no NDJSON lines")
		}
	})

	t.Run("upstream error", func(t *testing.T) {
		f := &fakeOllama{generateStatus: http.StatusInternalServerError, generateLine: `{"error":"boom"}`}
		srv := httptest.NewServer(f.handler())
		t.Cleanup(srv.Close)
		c := newWiredClient(t, srv)

		if _, err := c.Generate(t.Context(), "m", "hello", nil); err == nil {
			t.Fatal("expected an error when Ollama returns a non-2xx status")
		}
	})

	t.Run("multiple response lines with Stream=false", func(t *testing.T) {
		line1, err := json.Marshal(map[string]any{"model": "m", "response": "first", "done": false})
		if err != nil {
			t.Fatal(err)
		}
		line2, err := json.Marshal(map[string]any{"model": "m", "response": "", "done": true})
		if err != nil {
			t.Fatal(err)
		}
		f := &fakeOllama{generateLine: string(line1), generateLine2: string(line2)}
		srv := httptest.NewServer(f.handler())
		t.Cleanup(srv.Close)
		c := newWiredClient(t, srv)

		if _, err := c.Generate(t.Context(), "m", "hello", nil); err == nil {
			t.Fatal("expected an error when the server sends more than one response line")
		}
	})

	t.Run("truncated at length limit", func(t *testing.T) {
		line, err := json.Marshal(map[string]any{
			"model": "m", "response": `{"reply": "cut off mid-str`, "done": true, "done_reason": "length",
		})
		if err != nil {
			t.Fatal(err)
		}
		f := &fakeOllama{generateLine: string(line)}
		srv := httptest.NewServer(f.handler())
		t.Cleanup(srv.Close)
		c := newWiredClient(t, srv)

		if _, err := c.Generate(t.Context(), "m", "hello", nil); err == nil {
			t.Fatal("expected an error when done_reason is length")
		}
	})
}

func TestClient_notStarted(t *testing.T) {
	// Inject alone (no Start) must leave the client in a state that returns
	// errors, not panics — this is the state a concurrently-running Start
	// on another goroutine, or a misuse outside app.Bootstrap, would see.
	f := &fakeOllama{embedResp: &embedResponse{Model: "m", Embeddings: [][]float32{{0.1}}}}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)

	reg := prometheus.NewRegistry()
	metrics := &clienthttp.HTTPClientMetrics{}
	if err := metrics.RegisterMetrics(reg); err != nil {
		t.Fatal(err)
	}
	httpBuilt, err := (&clienthttp.Config{
		Label:   commonv1.Label{Value: "ollama"},
		BaseURL: httpv1.URL{Value: srv.URL},
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	httpClient := httpBuilt.(*clienthttp.Client)
	httpClient.Inject([]any{metrics})
	t.Cleanup(func() { _ = httpClient.Close(context.Background()) })

	c := &clientollama.Client{}
	c.Inject([]any{httpClient}) // no Start

	if _, err := c.Embed(t.Context(), "m", "hello"); err == nil {
		t.Fatal("expected Embed to error before Start")
	}
	if _, err := c.Generate(t.Context(), "m", "hello", nil); err == nil {
		t.Fatal("expected Generate to error before Start")
	}
	if err := c.ProbeReady(t.Context()); err == nil {
		t.Fatal("expected ProbeReady to error before Start")
	}
}

func TestClient_Start_malformedBaseURL(t *testing.T) {
	// Bypasses ecfg entirely, same as newWiredClient does for every other
	// test — Config.Build only rejects an empty string, not a malformed one
	// (see Start's doc comment). This exercises Start's url.Parse error
	// branch directly.
	httpBuilt, err := (&clienthttp.Config{
		Label:   commonv1.Label{Value: "ollama"},
		BaseURL: httpv1.URL{Value: "http://example.com:notaport"},
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	httpClient := httpBuilt.(*clienthttp.Client)

	reg := prometheus.NewRegistry()
	metrics := &clienthttp.HTTPClientMetrics{}
	if err := metrics.RegisterMetrics(reg); err != nil {
		t.Fatal(err)
	}
	httpClient.Inject([]any{metrics})
	t.Cleanup(func() { _ = httpClient.Close(context.Background()) })

	c := &clientollama.Client{}
	c.Inject([]any{httpClient})

	if err := c.Start(context.Background()); err == nil {
		t.Fatal("expected Start to reject a malformed BaseURL")
	}
}

func TestClient_StartRaceWithProbeReady(t *testing.T) {
	// Regression: runner.Runner.Run starts every Starter concurrently.
	// A readiness endpoint (itself a separate Starter, on its own goroutine)
	// can call ProbeReady on this Client before this Client's own Start has
	// returned. Must never race or panic — either a clean result or a
	// "not started" error is acceptable, depending on scheduling.
	f := &fakeOllama{}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)

	reg := prometheus.NewRegistry()
	metrics := &clienthttp.HTTPClientMetrics{}
	if err := metrics.RegisterMetrics(reg); err != nil {
		t.Fatal(err)
	}
	httpBuilt, err := (&clienthttp.Config{
		Label:   commonv1.Label{Value: "ollama"},
		BaseURL: httpv1.URL{Value: srv.URL},
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	httpClient := httpBuilt.(*clienthttp.Client)
	httpClient.Inject([]any{metrics})
	t.Cleanup(func() { _ = httpClient.Close(context.Background()) })

	c := &clientollama.Client{}
	c.Inject([]any{httpClient})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = c.Start(context.Background())
	}()
	go func() {
		defer wg.Done()
		_ = c.ProbeReady(context.Background()) // result ignored: either outcome is valid
	}()
	wg.Wait()
}

func TestClient_ProbeReady(t *testing.T) {
	t.Run("reachable", func(t *testing.T) {
		f := &fakeOllama{}
		srv := httptest.NewServer(f.handler())
		t.Cleanup(srv.Close)
		c := newWiredClient(t, srv)

		if err := c.ProbeReady(t.Context()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		f := &fakeOllama{heartbeatErr: true}
		srv := httptest.NewServer(f.handler())
		t.Cleanup(srv.Close)
		c := newWiredClient(t, srv)

		if err := c.ProbeReady(t.Context()); err == nil {
			t.Fatal("expected an error when the backend reports unhealthy")
		}
	})
}
