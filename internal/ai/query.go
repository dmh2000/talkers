package ai

import (
	"context"
	"fmt"

	llmclient "github.com/dmh2000/go-llmclient"
)

// Client is the LLM client interface exposed to callers.
type Client = llmclient.Client

// AIClient creates a new LLM client for the given model name.
// It resolves the model's provider and returns the configured client.
func AIClient(model string) (Client, error) {
	provider, err := llmclient.GetProviderName(model)
	if err != nil {
		return nil, err
	}
	return llmclient.NewClient(provider)
}

// AIQuery executes a text query against the given LLM client.
func AIQuery(client Client, systemPrompt string, queryContext []string, model string) (string, error) {
	return client.QueryText(context.Background(), systemPrompt, queryContext, model, llmclient.Options{})
}

// AIAddContext wraps content with XML-style ID tags and appends it to the query context.
func AIAddContext(queryContext []string, id string, content string) []string {
	s := fmt.Sprintf("<%s>\n%s\n</%s>", id, content, id)
	queryContext = append(queryContext, s)
	return queryContext
}
