package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// ===== Options =====

// dialConfig holds the accumulated ClientOption settings applied before dialing.
type dialConfig struct {
	certFile       string
	keyFile        string
	caFile         string
	plaintext      bool
	staticKey      string
	connectTimeout time.Duration
	interceptors   []grpc.UnaryClientInterceptor
}

// Option configures a Client before it dials.
type Option func(*dialConfig)

// WithMTLS configures mTLS transport credentials. The certFile and keyFile parameters are the
// client certificate and private key paths; caFile is the CA certificate used to verify the server.
// TLS 1.3 is the minimum version. Mutually exclusive with WithPlaintext.
func WithMTLS(certFile, keyFile, caFile string) Option {
	return func(c *dialConfig) {
		c.certFile = certFile
		c.keyFile = keyFile
		c.caFile = caFile
	}
}

// WithPlaintext disables transport security. Use only against servers running with TLS_MODE=disabled.
// Mutually exclusive with WithMTLS.
func WithPlaintext() Option {
	return func(c *dialConfig) { c.plaintext = true }
}

// WithStaticKey injects key as the x-api-key gRPC metadata header on every call. Use with servers
// running with TLS_MODE=disabled and TOKEN_ENGINE_STATIC_CALLER_KEYS configured.
func WithStaticKey(key string) Option {
	return func(c *dialConfig) { c.staticKey = key }
}

// WithConnectTimeout sets the minimum time the underlying transport waits when establishing a
// connection. Defaults to 10s if not set.
func WithConnectTimeout(d time.Duration) Option {
	return func(c *dialConfig) { c.connectTimeout = d }
}

// WithUnaryInterceptor appends a unary client interceptor. Multiple interceptors are chained in
// the order they are provided. Static-key injection (WithStaticKey) is always prepended before
// any interceptors added here.
func WithUnaryInterceptor(i grpc.UnaryClientInterceptor) Option {
	return func(c *dialConfig) { c.interceptors = append(c.interceptors, i) }
}

// ===== Concrete Type =====

// client is the concrete Client implementation.
type client struct {
	conn *grpc.ClientConn          // nil when created via NewClientFromStub
	stub tokenv1.TokenEngineClient
}

// NewClient constructs a Client that dials addr using the provided options. Returns
// ErrNoCredentials if neither WithMTLS nor WithPlaintext is provided. Returns a wrapped
// error if TLS credential files cannot be loaded or parsed.
func NewClient(addr string, opts ...Option) (Client, error) {
	// ===== STEP 1: Apply Options =====
	cfg := &dialConfig{
		connectTimeout: 10 * time.Second,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// ===== STEP 2: Validate Inputs =====
	if !cfg.plaintext && cfg.certFile == "" {
		return nil, ErrNoCredentials
	}

	// ===== STEP 3: Build Transport Credentials =====
	var creds credentials.TransportCredentials
	if cfg.plaintext {
		creds = insecure.NewCredentials()
	} else {
		var err error
		creds, err = buildTLSCredentials(cfg)
		if err != nil {
			return nil, err
		}
	}

	// ===== STEP 4: Assemble Dial Options =====
	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithConnectParams(grpc.ConnectParams{
			MinConnectTimeout: cfg.connectTimeout,
		}),
	}

	interceptors := cfg.interceptors
	if cfg.staticKey != "" {
		interceptors = append([]grpc.UnaryClientInterceptor{staticKeyInterceptor(cfg.staticKey)}, interceptors...)
	}
	if len(interceptors) > 0 {
		dialOpts = append(dialOpts, grpc.WithChainUnaryInterceptor(interceptors...))
	}

	// ===== STEP 5: Dial =====
	conn, err := grpc.NewClient(addr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	// ===== STEP 6: Return =====
	return &client{
		conn: conn,
		stub: tokenv1.NewTokenEngineClient(conn),
	}, nil
}

// NewClientFromStub constructs a Client backed by the given stub for testing purposes only.
func NewClientFromStub(stub tokenv1.TokenEngineClient) Client {
	return &client{stub: stub}
}

// ===== RPC Methods =====

// IssueToken issues a new token pair for the given subject and tenant.
// Returns the context error if the context is cancelled.
func (c *client) IssueToken(ctx context.Context, req *tokenv1.IssueTokenRequest) (*tokenv1.TokenPair, error) {
	return c.stub.IssueToken(ctx, req)
}

// RefreshToken exchanges a refresh token for a new token pair.
// Returns the context error if the context is cancelled.
func (c *client) RefreshToken(ctx context.Context, req *tokenv1.RefreshTokenRequest) (*tokenv1.TokenPair, error) {
	return c.stub.RefreshToken(ctx, req)
}

// RevokeToken revokes a single refresh token by value.
// Returns the context error if the context is cancelled.
func (c *client) RevokeToken(ctx context.Context, req *tokenv1.RevokeTokenRequest) (*tokenv1.RevokeTokenResponse, error) {
	return c.stub.RevokeToken(ctx, req)
}

// RevokeAllForAudience revokes all refresh tokens for the given audience within a tenant.
// Returns the context error if the context is cancelled.
func (c *client) RevokeAllForAudience(ctx context.Context, req *tokenv1.RevokeAudienceRequest) (*tokenv1.RevokeTokenResponse, error) {
	return c.stub.RevokeAllForAudience(ctx, req)
}

// RevokeAllUserTokens revokes all refresh tokens for the given user within a tenant.
// Returns the context error if the context is cancelled.
func (c *client) RevokeAllUserTokens(ctx context.Context, req *tokenv1.RevokeUserRequest) (*tokenv1.RevokeTokenResponse, error) {
	return c.stub.RevokeAllUserTokens(ctx, req)
}

// RevokeAllForUserAndAudience revokes all refresh tokens for the given user and audience within a tenant.
// Returns the context error if the context is cancelled.
func (c *client) RevokeAllForUserAndAudience(ctx context.Context, req *tokenv1.RevokeUserAndAudienceRequest) (*tokenv1.RevokeTokenResponse, error) {
	return c.stub.RevokeAllForUserAndAudience(ctx, req)
}

// Close tears down the underlying gRPC connection. It is idempotent — calling Close on a
// stub-backed client (created via NewClientFromStub) is a no-op.
func (c *client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// ===== Helpers =====

func buildTLSCredentials(cfg *dialConfig) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(cfg.certFile, cfg.keyFile)
	if err != nil {
		return nil, fmt.Errorf("load client cert/key: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, ErrInvalidCA
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS13,
	}), nil
}

func staticKeyInterceptor(key string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", key)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
