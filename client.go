package clientollama

import (
	"fmt"
	"net/url"

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
	http *clienthttp.Client
	api  *ollamaapi.Client
}

var _ app.ResourceFactory = (*Client)(nil)
var _ app.StandBy = (*Client)(nil)

func (*Client) NewResource() (any, error) { return &Client{}, nil }

func (*Client) Deps() []any { return []any{(*clienthttp.Client)(nil)} }

// Inject only stores the dependency pointer. sdi.Resolve runs Inject across
// every resource in a single pass ordered by registration, not by the
// Deps() graph — a ResourceFactory (this type) is always registered before
// any Configurable (client-http.Client) materializes, regardless of catalog
// field order. Reading dep.HTTPClient() here would observe it before
// client-http.Client's own Inject has set it. StandBy runs later, once
// sdi.Resolve has finished entirely (every resource's Inject has run),
// which is where dep's state is safe to consume.
func (c *Client) Inject(args []any) {
	for _, arg := range args {
		if dep, ok := arg.(*clienthttp.Client); ok {
			c.http = dep
		}
	}
}

// StandBy builds the Ollama SDK client from the now fully-wired client-http.Client.
// It runs once, sequentially, after sdi.Resolve and before runner.Run starts
// the concurrent Start phase — no I/O happens here, so there is no ordering
// hazard to guard against the way there would be for a runner.Starter.
//
// The url.Parse error below is not expected to trigger when this Client was
// wired through app.Bootstrap's normal pipeline: ecfg.LoadEnv validates
// BaseURL via protovalidate (http.v1.URL's string.uri rule) before
// Config.Build ever runs. That is a calling-convention guarantee, not a
// type-safety one — clienthttp.Config's own fields are exported and
// Config.Build itself only rejects an empty string, so anything calling
// Build directly (as this package's own tests do) bypasses ecfg entirely.
// See TestMarkClientHTTPLoadEnvRejectsMalformedBaseURL, which pins the
// ecfg.LoadEnv side of this assumption, and TestClient_StandBy_malformedBaseURL,
// which exercises this error branch directly via that same bypass.
func (c *Client) StandBy() error {
	base, err := url.Parse(c.http.BaseURL())
	if err != nil {
		return fmt.Errorf("clientollama: parse base URL: %w", err)
	}
	c.api = ollamaapi.NewClient(base, c.http.HTTPClient())
	return nil
}

// apiClient returns the SDK client built by StandBy, or an error if StandBy
// hasn't run yet — e.g. this Client's business methods called directly in a
// test, bypassing app.Bootstrap.
func (c *Client) apiClient() (*ollamaapi.Client, error) {
	if c.api == nil {
		return nil, fmt.Errorf("clientollama: not started")
	}
	return c.api, nil
}

// packageError wraps a passthrough error from the Ollama SDK with this
// package's prefix. Only for wrapping an existing error with nothing added —
// errors this package originates itself are built with fmt.Errorf directly.
func packageError(err error) error {
	return fmt.Errorf("clientollama: %w", err)
}
