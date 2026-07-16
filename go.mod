module github.com/muthuishere/agent-proxy

go 1.23.7

replace github.com/elazarl/goproxy => ./third_party/goproxywss

require (
	github.com/coder/websocket v1.8.14
	github.com/elazarl/goproxy v1.8.2
	github.com/gorilla/websocket v1.5.3
)

require (
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/text v0.28.0 // indirect
)
