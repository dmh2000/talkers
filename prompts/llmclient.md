@internal/ai/query.go  in the file internal/ai/query.go, do the following:
- import the github/dmh2000/llmclient library
- aiClient : call llmclient.newClient with one parameter 'model' and returns the llmClient object
- aiQuery : executes the llmclient QueryText function of the llmclient object. 
  - example : client.QueryText(context.Background(), systemPrompt, queryContext, model, llmclient.Options{})
  - include all the arguments

tell me if you need more information