module github.com/aetomala/token-engine/examples/custom-claims

go 1.26.5

require (
	github.com/aetomala/token-engine v0.8.0
	github.com/golang-jwt/jwt/v5 v5.3.1
)

require (
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/grpc v1.84.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/aetomala/token-engine => ../..
