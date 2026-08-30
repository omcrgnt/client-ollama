# client-ollama

Typed Ollama API client for omcrgnt apps: `Embed`, `Generate`, `ProbeReady`
(via `Heartbeat`) over an already-instrumented
[`client-http`](https://github.com/omcrgnt/client-http) client. No config of
its own — `Label`/`BaseURL` live entirely on the `client-http.Client` it
wraps.

Wraps [`github.com/ollama/ollama/api`](https://github.com/ollama/ollama) —
the official Ollama Go SDK — instead of hand-rolling request/response
marshaling.

## Catalog

```go
import (
	clienthttp "github.com/omcrgnt/client-http"
	clientollama "github.com/omcrgnt/client-ollama"
)

type catalog struct {
	OllamaHTTP *clienthttp.Client   `ecfg:"OLLAMA"`
	Ollama     *clientollama.Client
}
```

`Client` depends on the injected `*clienthttp.Client` (`Deps`/`Inject`) —
same slot, same env vars (`OLLAMA_LABEL`, `OLLAMA_BASE_URL`) as any other
`client-http` consumer. See client-http's README for those.

## Lifecycle

| Hook | Role |
|------|------|
| `NewResource` | Zero `*Client`; `Inject`/`StandBy` fill it in |
| `Deps` / `Inject` | Store the `*clienthttp.Client` pointer only — no computation here |
| `StandBy` | Build the Ollama SDK client from `HTTPClient()` + `BaseURL()` |
| `ProbeReady` | `Heartbeat` (`HEAD /`) — cheap, side-effect-free reachability check |

`Inject` deliberately does nothing but store the pointer: `sdi.Resolve` runs
`Inject` across every resource in one pass ordered by registration, not by
the `Deps()` graph, so `client-http.Client`'s own `Inject` isn't guaranteed
to have run yet. `StandBy` runs later, once `sdi.Resolve` has finished
entirely (every resource's `Inject` has run) — see the lifecycle safety rule
in `github.com/omcrgnt/app`'s package doc. Building the SDK client in
`Inject` instead would reintroduce that ordering race.

No `Config`/`BuildConfig`, no `runner.Starter`: nothing here owns an
env-configured identity or does I/O — `StandBy` is a sequential, zero-I/O
hook (see `runner.StandBy`'s doc), not a `runner.Starter`. `StandBy` returns
a nil cleanup — no `Close` either — since `Client` owns no separate resource
of its own to release, only a wrapper around the already-owned `*http.Client`
behind `clienthttp.Client`.

## API

```go
vec, err := c.Embed(ctx, "all-minilm", "some text")

reply, err := c.Generate(ctx, "llama3", prompt, json.RawMessage(`"json"`))
```

`Generate`'s `schema` parameter maps directly to Ollama's structured-output
`format` field: `nil` for plain text, `json.RawMessage(`"json"`)` for
unstructured-but-valid JSON, or a JSON Schema object for strict structure.
Non-streaming only (`Stream: false`) — one response in, one string out.

## Metrics & tracing

Come entirely from the wrapped `client-http.Client` — every `Embed`/
`Generate`/`ProbeReady` call goes through its instrumented transport, so
`http_client_request_duration_seconds{client="...",...}` and otel spans
already cover this client. Nothing extra to wire.
