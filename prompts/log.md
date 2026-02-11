## Add Loging to server

- log output should show filename and line number 
- log output should output to stdout
- log client connections, client deletions, errors 
- log when clients send messages. the log output should state the sending client and the receiving client. do not log the actual message contents

## Makefiles
- create a heirarchy of Makefiles from the top level to each subdirectory that contains .go files
- each makefile should contain rules for:
  - 'lint'
    - call golangci-lint to the files in the directory
  - 'test'
    - execute any tests if there are any. if the directory has no tests, output 'no tests'
  - 'build'
    - if the directory has a main.go file , execute go build 
    - if no main.go file, output 'no build'
    - in directory 'internal/proto', rebuild talkers.pb.go if talkers.proto has changed 
  - 'clean'
    - remove build artifacts, including executable files that are built 
  - 'all'
    - execute 'clean', 'lint' and 'build' 

  
---
client output

make this change to client/main.go:
- use terminal control codes to enable color output
- when printing the output of the messages from other clients (readLoop) the output text should be dark blue
- when taking input from the command line, text should be colored light green