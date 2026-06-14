module github.com/aetomala/token-engine/examples/custom-claims

go 1.26.4

require (
	github.com/aetomala/token-engine v0.8.0
	github.com/golang-jwt/jwt/v5 v5.3.1
)

require (
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260406210006-6f92a3bedf2d // indirect
	google.golang.org/grpc v1.81.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/aetomala/token-engine => ../..
