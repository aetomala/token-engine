module github.com/aetomala/token-engine/examples/custom-claims

go 1.26.5

require (
	github.com/aetomala/token-engine v0.8.0
	github.com/golang-jwt/jwt/v5 v5.3.1
)

require (
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/grpc v1.82.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/aetomala/token-engine => ../..
