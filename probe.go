package clientollama

import "context"

// ProbeReady reports whether the Ollama backend is reachable (ops/probe
// duck typing — see client-http's README for why this lives here and not
// on client-http itself: readiness is API-specific, and client-http has no
// idea what "up" means for the API it transports).
func (c *Client) ProbeReady(ctx context.Context) error {
	api, err := c.api()
	if err != nil {
		return err
	}
	if err := api.Heartbeat(ctx); err != nil {
		return packageError(err)
	}
	return nil
}
