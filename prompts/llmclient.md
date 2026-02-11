@internal/ai/query.go  in the file internal/ai/query.go, do the following:
- import the github/dmh2000/llmclient library
- aiClient : call llmclient.newClient with one parameter 'model' and returns the llmClient object
- aiQuery : executes the llmclient QueryText function of the llmclient object. 
  - example : client.QueryText(context.Background(), systemPrompt, queryContext, model, llmclient.Options{})
  - include all the arguments

tell me if you need more information


@main.go @internal/ai/query.go
- in main.go, do the following:
  - right after the model variable is set, create a variable "content := []string{} // context for AI queries"
  - using the model name, create an aiClient
  - in 'readLoop', when an Envelope_Message is received, add it to the 'content' variable using the aiAddContent function
  - in the main function, add the message conent to the 'content' variable using aiAddConent function
  - since the content is being updated by both main loop and read loop, it probably needs a mutext around the aiAddConent function. let me know if there is a better way to handle that


@main.go i made a mistake. the 'system' argument will be a filename. read the file and 
  add its contents to a  'system' variable.  make the 'system' and 'model' argument       
  required instead of using dummy values. use 'system' instead of 'systemPrompt' as the   
  variable name. if the system file cannot be read then output an error message and exit