package clientollama

import (
	"context"
	"fmt"

	ollamaapi "github.com/ollama/ollama/api"
)

func (c *Client) Embed(ctx context.Context, model, text string) ([]float32, error) {
	api, err := c.apiClient()
	if err != nil {
		return nil, err
	}
	resp, err := api.Embed(ctx, &ollamaapi.EmbedRequest{Model: model, Input: text})
	if err != nil {
		return nil, packageError(err)
	}
	// External API boundary: guard against a malformed/empty response
	// instead of panicking on resp.Embeddings[0].
	if len(resp.Embeddings) == 0 || len(resp.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("clientollama: empty embeddings response for model %q", model)
	}
	return resp.Embeddings[0], nil
}
