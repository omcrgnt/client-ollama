package clientollama

import (
	"context"
	"fmt"
	"net/url"
	"sync/atomic"

	ollamaapi "github.com/ollama/ollama/api"
	"github.com/omcrgnt/app"
	clienthttp "github.com/omcrgnt/client-http"
)

// Client is a typed Ollama API client (Embed/Generate/Heartbeat) over an
// already-instrumented client-http.Client. It has no ecfg config of its
// own: Label and BaseURL live entirely on the injected client-http.Client,
// same slot the app configures via ecfg (e.g. `ecfg:"OLLAMA"`).
//
// Catalog field: *Client (ResourceFactory).
type Client struct {
	http   *clienthttp.Client
	apiPtr atomic.Pointer[ollamaapi.Client]
}

var _ app.ResourceFactory = (*Client)(nil)

func (*Client) NewResource() (any, error) { return &Client{}, nil }

func (*Client) Deps() []any { return []any{(*clienthttp.Client)(nil)} }

// Inject only stores the dependency pointer. sdi.Resolve runs Inject across
// every resource in a single pass ordered by registration, not by the
// Deps() graph — a ResourceFactory (this type) is always registered before
// any Configurable (client-http.Client) materializes, regardless of catalog
// field order. Reading dep.HTTPClient() here would observe it before
// client-http.Client's own Inject has set it. Start runs later, guaranteed
// after every Inject has completed (via runner.Runner), which is where
// dep's state is safe to consume.
func (c *Client) Inject(args []any) {
	for _, arg := range args {
		if dep, ok := arg.(*clienthttp.Client); ok {
			c.http = dep
		}
	}
}

// Start builds the Ollama SDK client from the now fully-wired client-http.Client.
//
// The url.Parse error below is not expected to trigger when this Client was
// wired through app.Bootstrap's normal pipeline: ecfg.LoadEnv validates
// BaseURL via protovalidate (http.v1.URL's string.uri rule) before
// Config.Build ever runs. That is a calling-convention guarantee, not a
// type-safety one — clienthttp.Config's own fields are exported and
// Config.Build itself only rejects an empty string, so anything calling
// Build directly (as this package's own tests do) bypasses ecfg entirely.
// See TestMarkClientHTTPLoadEnvRejectsMalformedBaseURL, which pins the
// ecfg.LoadEnv side of this assumption, and TestClient_Start_malformedBaseURL,
// which exercises this error branch directly via that same bypass.
func (c *Client) Start(context.Context) error {
	base, err := url.Parse(c.http.BaseURL())
	if err != nil {
		return fmt.Errorf("clientollama: parse base URL: %w", err)
	}
	c.apiPtr.Store(ollamaapi.NewClient(base, c.http.HTTPClient()))
	return nil
}

// api returns the SDK client built by Start, or an error if Start hasn't
// completed yet — reachable in practice, not just in theory:
// runner.Runner.Run starts every Starter concurrently, so a readiness probe
// on this same Client can run on another goroutine before this Client's own
// Start has returned.
func (c *Client) api() (*ollamaapi.Client, error) {
	api := c.apiPtr.Load()
	if api == nil {
		return nil, fmt.Errorf("clientollama: not started")
	}
	return api, nil
}

// packageError wraps a passthrough error from the Ollama SDK with this
// package's prefix. Only for wrapping an existing error with nothing added —
// errors this package originates itself are built with fmt.Errorf directly.
func packageError(err error) error {
	return fmt.Errorf("clientollama: %w", err)
}
