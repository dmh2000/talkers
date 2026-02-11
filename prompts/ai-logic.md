# logic for AI
@client/main.go @internal/ai/query.go

## 1 change the names of the following variables as specified:
  - in main.go and query.go, chaange "AIAddContent" to "AIAddContext"
  - in main.go, change "content" to "queryContext"
  - in main.go, change the name "maxContentLength" to "maxInputLength" and set its value to 256 characters 

## 2 modify the writeLoop text input
  - the writeLoop will now have two sources of input, the terminal and the response from AIQuery
  - the writeloop will read input from a new channel instead of directly from the terminal
  - add a mutex if necessary

## 3 terminal input
  - the terminal input should be placed in a separate go routine, and send its input to the write loop channel

## 4 AIQuery output
  - in the readLoop section "case *pb.Envelope_Message:" after a message is received, add the response message to the aiContext (that is already implemented)
  - call "AIQuery"  with:
    - updated aiContext
    - the sending client id
    - the contents of the 'system' command line argument
  - send the output of the AIQuery function to the writeloop input channel

If you see any problems, stop and ask me before proceeding