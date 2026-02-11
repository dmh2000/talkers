package ai

import (
	"context"
	"fmt"

	llmclient "github.com/dmh2000/go-llmclient"
)

// AIClient creates a new LLM client for the given model name.
// It resolves the model's provider and returns the configured client.
func AIClient(model string) (llmclient.Client, error) {
	provider, err := llmclient.GetProviderName(model)
	if err != nil {
		return nil, err
	}
	return llmclient.NewClient(provider)
}

// AIQuery executes a text query against the given LLM client.
func AIQuery(client llmclient.Client, systemPrompt string, queryContext []string, model string) (string, error) {
	return client.QueryText(context.Background(), systemPrompt, queryContext, model, llmclient.Options{})
}

// AIAddContent wraps content with XML-style ID tags and appends it to the query context.
func AIAddContent(queryContext []string, id string, content string) []string {
	s := fmt.Sprintf("<%s>\n%s\n</%s>", id, content, id)
	queryContext = append(queryContext, s)
	return queryContext
}
