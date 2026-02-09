## create native client and server cli apps
### purpose
  - clients can send messages to any other connected client
  - the server acts as a message broker, receiving messages from a client and routing them out to other clients

### details
- use the go programming language
- use github.com/quic-go/quic-go
- the server and clients are command line applications
- write the code for the client in the 'client' directory
- write the code for the server in the 'server' directory
- write any internal modules in the 'internal' directory
- write any tests to the 'test' directory
- server command line arguments:
  - ip:port (string)
- client command line arguments:
  - client id (string)
  - server ip:port (string)

### functionality
  - use QUIC transport
  - Generate a self‑signed certificate at startup using Go’s crypto/x509 APIs with any hostname 'sqirvy.xyz'
  - the clients shall use the InsecureSkipVerify when connecting
  - on first connect by a client to a server, the client sends a REGISTER  message to server to register it's client id. 
  - Server keeps map[userID]*ClientConn where ClientConn wraps the quic.Connection and primary quic.Stream.
  - If a client disconnects or does not respond in a defined timeout (to be specified in the code, later), the server removes its id from the map
  - One bidirectional stream per client session.
  - Client writes protobuf messages  to that stream.
  - Server,  in a loop receives messages and forwards them to the designated client. 
  - if a destination client is not registered, the server sends an error message to the sending client
  - if a client attempts to register a client id that is already registered, the server will reject the connection with an error message to the offending duplicate client
  - the server will support up to 16 clients.
  - the server can be killed by a ctrl-c. it should terminate cleanly by first closing all client connections
  - clients can be killed by a ctrl-c. if the server detects the disconnect, it will remove the client. otherwise the server will remove the client connection the next time it attempts to send to a disconnected client and send the requesting client an error
  - if a client is disconnected by the server, it shall print an error message to the console and terminate
  - clients will not attempt to reconnect if a connection is lost. it will be up to the operator to restart the client if desired
  - no support for discovery. instead clients will just attempt to send messages and if it receives an error from the server. the client will log the error and terminate
  - initially, for test purposes, the client will read characters from the command line and send messages when the operators hits the 'enter' key. the actual functionality of the client will be specifed and implemented later.   

### Messages

there is one type of message, with a 'type', 'from', 'to' and content string. the string will be up to 250,000 characters  long.
clients specify their own client id. 
message format is protobuf
there is only one message format

use the protobuf 'oneof' to wrap the different messages

This is a pseudocode spec for the protobuf messages. use this as a spec for creating the real one
```
message REGISTER {
    string from        (any string up to 32 characters long)
}

message ERROR {
    string error
}

message MESSAGE {
    string from_id (id of the sending client)
    string to_id (id of the destination client)
    string content
}
```

#### Errors
- if a client sends a content larger than 250,000 characters, the server shall discard the message and return an error to the sending client
- if a client sends a message to a client that is not registered, the server shall return an error to the sending client
- if a client attempts to register when there are already 16 connected clients, the server shall return an error

error are strings, and will be defined together in the code
