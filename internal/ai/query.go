package ai

import (
	"context"

	llmclient "github.com/dmh2000/go-llmclient"
)

// aiClient creates a new LLM client for the given model name.
// It resolves the model's provider and returns the configured client.
func aiClient(model string) (llmclient.Client, error) {
	provider, err := llmclient.GetProviderName(model)
	if err != nil {
		return nil, err
	}
	return llmclient.NewClient(provider)
}

// aiQuery executes a text query against the given LLM client.
func aiQuery(client llmclient.Client, systemPrompt string, queryContext []string, model string) (string, error) {
	return client.QueryText(context.Background(), systemPrompt, queryContext, model, llmclient.Options{})
}
