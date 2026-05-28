FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o token-engine ./cmd/token-engine

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /app/token-engine /token-engine
ENTRYPOINT ["/token-engine"]
