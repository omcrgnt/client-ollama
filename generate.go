package clientollama

import (
	"context"
	"encoding/json"
	"fmt"

	ollamaapi "github.com/ollama/ollama/api"
)

// Generate produces a single, non-streaming response for prompt using
// model. schema constrains the response via Ollama's structured-output
// "format" field: pass json.RawMessage(`"json"`) for unstructured-but-valid
// JSON, a JSON Schema object for strict structure, or nil for plain text.
func (c *Client) Generate(ctx context.Context, model, prompt string, schema json.RawMessage) (string, error) {
	api, err := c.api()
	if err != nil {
		return "", err
	}

	// Stream must stay a non-nil pointer to false: Ollama treats a nil Stream
	// as true (streamed, multiple response lines). The single-response
	// assumption below — one callback invocation, no accumulation — only
	// holds as long as this stays new(bool).
	req := &ollamaapi.GenerateRequest{
		Model:  model,
		Prompt: prompt,
		Stream: new(bool),
		Format: schema,
	}

	var out string
	var got bool
	var doneReason string
	err = api.Generate(ctx, req, func(resp ollamaapi.GenerateResponse) error {
		if got {
			// Unprefixed: this flows back out through api.Generate's own
			// return value below, alongside genuine SDK/transport errors,
			// and gets the "clientollama:" prefix applied uniformly there —
			// prefixing it here too would double it up.
			return fmt.Errorf("multiple response lines for model %q with Stream=false", model)
		}
		out = resp.Response
		doneReason = resp.DoneReason
		got = true
		return nil
	})
	if err != nil {
		return "", packageError(err)
	}
	// External API boundary: Stream=false should always deliver exactly one
	// response, but don't trust that silently — an empty out could also
	// mean "call succeeded, callback truly never ran".
	if !got {
		return "", fmt.Errorf("clientollama: no response received for model %q", model)
	}
	// DoneReason "length" means the model hit its context/output limit and
	// out is truncated mid-token-stream — for the structured-JSON use case
	// this package advertises, that's an unparseable fragment, not a usable
	// (if imperfect) answer. Surface it as an error instead of returning
	// truncated text indistinguishable from a clean completion.
	if doneReason == "length" {
		return "", fmt.Errorf("clientollama: response truncated (done_reason=length) for model %q", model)
	}
	return out, nil
}
