package bench_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"github.com/aetomala/token-engine/internal/audit"
	"github.com/aetomala/token-engine/internal/config"
	"github.com/aetomala/token-engine/internal/handler"
	"github.com/aetomala/token-engine/internal/interceptor"
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/registry"
	"github.com/aetomala/token-engine/internal/store"
)

var (
	// No-TLS stack.
	benchClient tokenv1.TokenEngineClient
	benchConn   *grpc.ClientConn
	benchServer *grpc.Server

	// Mutual TLS stack — same handler, different transport.
	mtlsClient tokenv1.TokenEngineClient
	mtlsConn   *grpc.ClientConn
	mtlsServer *grpc.Server

	benchKM interface{ Shutdown(context.Context) error }
	benchMR *miniredis.Miniredis
)

func TestMain(m *testing.M) {
	if err := setup(); err != nil {
		panic("bench setup: " + err.Error())
	}
	code := m.Run()
	teardown()
	os.Exit(code)
}

func setup() error {
	// ===== miniredis =====
	var err error
	benchMR, err = miniredis.Run()
	if err != nil {
		return err
	}

	// ===== Config =====
	cfg := &config.Config{
		Issuer:           "test-issuer",
		Audience:         "api",
		TLSMode:          "disabled",
		StaticCallerKeys: map[string]string{"test-api-key": "test-caller"},
		RedisAddr:        benchMR.Addr(),
		IdempotencyTTL:   24 * time.Hour,
		JWKSCacheMaxAge:  5 * time.Minute,
	}

	// ===== Observability (NoOp — not measured here) =====
	logger := observability.NewNoOpLogger()
	tracer := observability.NewNoOpTracer()
	metrics := observability.NewNoOpMetrics()

	// ===== Redis =====
	redisClient := redis.NewClient(&redis.Options{Addr: benchMR.Addr()})

	// ===== Tenant registry — key generation can take up to 30s on first run =====
	initCtx, initCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer initCancel()
	promReg := prometheus.NewRegistry()
	tenantReg := registry.NewMultiTenantRegistry(redisClient, promReg, logger, tracer, metrics)
	if err := tenantReg.Add(initCtx, cfg.Issuer, registry.TenantConfig{
		Issuer:   cfg.Issuer,
		Audience: cfg.Audience,
	}); err != nil {
		return err
	}
	benchKM = tenantReg.AllKeyManagers()[cfg.Issuer]

	// ===== Shared handler (auth + idempotency + validation interceptors) =====
	idempStore := store.NewRedisIdempotencyStore(redisClient, cfg.IdempotencyTTL)
	auth := interceptor.NewStaticKeyAuthenticator(cfg.StaticCallerKeys)
	callerReg := registry.NewStaticCallerRegistry(&registry.CallerRegistryConfig{
		Version: 1,
		Callers: []registry.CallerEntry{
			{Identity: "test-caller", PermittedTenants: []string{"test-issuer"}},
		},
	}, logger)
	tokenHandler := handler.NewTokenHandler(tenantReg, audit.NewNoOpAuditStore(), logger, tracer, metrics)

	chainOpts := func() grpc.ServerOption {
		return grpc.ChainUnaryInterceptor(
			observability.NewCorrelationInterceptor(logger, metrics),
			interceptor.NewAuthInterceptor(auth, logger),
			interceptor.NewCallerAuthorizationInterceptor(callerReg, logger),
			interceptor.NewIdempotencyInterceptor(idempStore, logger, metrics),
			interceptor.NewValidationInterceptor(logger),
		)
	}

	// ===== No-TLS gRPC server =====
	benchServer = grpc.NewServer(chainOpts())
	tokenv1.RegisterTokenEngineServer(benchServer, tokenHandler)
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return err
	}
	go func() { _ = benchServer.Serve(ln) }()
	benchConn, err = grpc.NewClient(ln.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	benchClient = tokenv1.NewTokenEngineClient(benchConn)

	// ===== mTLS gRPC server =====
	serverTLSCfg, clientTLSCfg, err := generateTLSCerts()
	if err != nil {
		return err
	}
	mtlsServer = grpc.NewServer(chainOpts(), grpc.Creds(credentials.NewTLS(serverTLSCfg)))
	tokenv1.RegisterTokenEngineServer(mtlsServer, tokenHandler)
	// Bind to 127.0.0.1 explicitly so the TCP address matches the cert's IP SAN.
	mtlsLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	go func() { _ = mtlsServer.Serve(mtlsLn) }()
	mtlsConn, err = grpc.NewClient(mtlsLn.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLSCfg)))
	if err != nil {
		return err
	}
	mtlsClient = tokenv1.NewTokenEngineClient(mtlsConn)

	return nil
}

func teardown() {
	if benchConn != nil {
		_ = benchConn.Close()
	}
	if benchServer != nil {
		benchServer.GracefulStop()
	}
	if mtlsConn != nil {
		_ = mtlsConn.Close()
	}
	if mtlsServer != nil {
		mtlsServer.GracefulStop()
	}
	if benchKM != nil {
		_ = benchKM.Shutdown(context.Background())
	}
	if benchMR != nil {
		benchMR.Close()
	}
}

// benchCtx returns a context carrying the test API key with a 5s timeout.
func benchCtx() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	return metadata.AppendToOutgoingContext(ctx, "x-api-key", "test-api-key"), cancel
}

// issueToken is a helper for pre-populating tokens in benchmark setup sections.
func issueToken(client tokenv1.TokenEngineClient, sub string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", "test-api-key")
	pair, err := client.IssueToken(ctx, &tokenv1.IssueTokenRequest{Sub: sub, TenantId: "test-issuer"})
	if err != nil {
		return "", err
	}
	return pair.RefreshToken, nil
}

// generateTLSCerts creates an in-memory CA, server cert, and client cert for mTLS benchmarks.
func generateTLSCerts() (serverCfg *tls.Config, clientCfg *tls.Config, err error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "bench-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, nil, err
	}

	serverTLSCert, err := signCert(big.NewInt(2), "bench-server",
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		[]net.IP{net.ParseIP("127.0.0.1")},
		[]string{"localhost"},
		caCert, caKey,
	)
	if err != nil {
		return nil, nil, err
	}

	clientTLSCert, err := signCert(big.NewInt(3), "bench-client",
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		nil, nil,
		caCert, caKey,
	)
	if err != nil {
		return nil, nil, err
	}

	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

	serverCfg = &tls.Config{
		Certificates: []tls.Certificate{serverTLSCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
		MinVersion:   tls.VersionTLS13,
	}
	clientCfg = &tls.Config{
		Certificates: []tls.Certificate{clientTLSCert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS13,
	}
	return serverCfg, clientCfg, nil
}

func signCert(
	serial *big.Int,
	cn string,
	extKeyUsage []x509.ExtKeyUsage,
	ips []net.IP,
	dns []string,
	parent *x509.Certificate,
	parentKey *ecdsa.PrivateKey,
) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  extKeyUsage,
		IPAddresses:  ips,
		DNSNames:     dns,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEM, keyPEM)
}

// ===== Benchmarks: bulk revocation (run first — must measure against a clean store) =====
// These benchmarks run before any large-scale token accumulation. Bulk revocation
// performance scales with the number of tokens in the store; the numbers below reflect
// the baseline RPC cost with a near-empty store.

func BenchmarkRevokeAllForAudience(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := benchCtx()
		_, err := benchClient.RevokeAllForAudience(ctx, &tokenv1.RevokeAudienceRequest{
			Audience: "api",
			TenantId: "test-issuer",
		})
		cancel()
		if err != nil {
			b.Fatalf("RevokeAllForAudience: %v", err)
		}
	}
}

func BenchmarkRevokeAllUserTokens(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := benchCtx()
		_, err := benchClient.RevokeAllUserTokens(ctx, &tokenv1.RevokeUserRequest{
			UserId:   "bench-user",
			TenantId: "test-issuer",
		})
		cancel()
		if err != nil {
			b.Fatalf("RevokeAllUserTokens: %v", err)
		}
	}
}

func BenchmarkRevokeAllForUserAndAudience(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := benchCtx()
		_, err := benchClient.RevokeAllForUserAndAudience(ctx, &tokenv1.RevokeUserAndAudienceRequest{
			UserId:   "bench-user",
			Audience: "api",
			TenantId: "test-issuer",
		})
		cancel()
		if err != nil {
			b.Fatalf("RevokeAllForUserAndAudience: %v", err)
		}
	}
}

// BenchmarkRevokeToken measures the combined issue-then-revoke path — each iteration
// issues a fresh token so the revocation operates on a live token.
func BenchmarkRevokeToken(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		issueCtx, issueCancel := benchCtx()
		pair, err := benchClient.IssueToken(issueCtx, &tokenv1.IssueTokenRequest{
			Sub:      "bench-revoke-user",
			TenantId: "test-issuer",
		})
		issueCancel()
		if err != nil {
			b.Fatalf("IssueToken (pre-revoke): %v", err)
		}

		revokeCtx, revokeCancel := benchCtx()
		_, err = benchClient.RevokeToken(revokeCtx, &tokenv1.RevokeTokenRequest{
			RefreshToken: pair.RefreshToken,
			TenantId:     "test-issuer",
		})
		revokeCancel()
		if err != nil {
			b.Fatalf("RevokeToken: %v", err)
		}
	}
}

// ===== Benchmarks: no-TLS single-client =====

func BenchmarkIssueToken(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := benchCtx()
		_, err := benchClient.IssueToken(ctx, &tokenv1.IssueTokenRequest{
			Sub:      "bench-user",
			TenantId: "test-issuer",
		})
		cancel()
		if err != nil {
			b.Fatalf("IssueToken: %v", err)
		}
	}
}

// BenchmarkRefreshToken measures the time for a single RefreshToken RPC. Because jwtauth
// rotates the refresh token on each use, a fresh token is pre-issued for each iteration
// in the setup phase (timer paused). The timed section measures only the RefreshToken RPC.
func BenchmarkRefreshToken(b *testing.B) {
	tokens := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		rt, err := issueToken(benchClient, "bench-refresh-user")
		if err != nil {
			b.Fatalf("pre-issue for refresh pool: %v", err)
		}
		tokens[i] = rt
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := benchCtx()
		_, err := benchClient.RefreshToken(ctx, &tokenv1.RefreshTokenRequest{
			RefreshToken: tokens[i],
			TenantId:     "test-issuer",
		})
		cancel()
		if err != nil {
			b.Fatalf("RefreshToken: %v", err)
		}
	}
}

// ===== Benchmarks: mTLS single-client =====

func BenchmarkIssueTokenMTLS(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := benchCtx()
		_, err := mtlsClient.IssueToken(ctx, &tokenv1.IssueTokenRequest{
			Sub:      "bench-user-mtls",
			TenantId: "test-issuer",
		})
		cancel()
		if err != nil {
			b.Fatalf("IssueToken (mTLS): %v", err)
		}
	}
}

// BenchmarkRefreshTokenMTLS applies the same pool strategy as BenchmarkRefreshToken.
func BenchmarkRefreshTokenMTLS(b *testing.B) {
	tokens := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		rt, err := issueToken(mtlsClient, "bench-refresh-user-mtls")
		if err != nil {
			b.Fatalf("pre-issue for mTLS refresh pool: %v", err)
		}
		tokens[i] = rt
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := benchCtx()
		_, err := mtlsClient.RefreshToken(ctx, &tokenv1.RefreshTokenRequest{
			RefreshToken: tokens[i],
			TenantId:     "test-issuer",
		})
		cancel()
		if err != nil {
			b.Fatalf("RefreshToken (mTLS): %v", err)
		}
	}
}

// ===== Benchmarks: no-TLS 10 concurrent clients =====

// BenchmarkIssueToken10Concurrent measures IssueToken throughput under exactly 10 concurrent
// clients dispatched via a fixed-size worker pool.
func BenchmarkIssueToken10Concurrent(b *testing.B) {
	const workers = 10
	work := make(chan struct{}, workers*2)
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		bErr error
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range work {
				ctx, cancel := benchCtx()
				_, err := benchClient.IssueToken(ctx, &tokenv1.IssueTokenRequest{
					Sub:      "bench-concurrent-user",
					TenantId: "test-issuer",
				})
				cancel()
				if err != nil {
					mu.Lock()
					bErr = err
					mu.Unlock()
				}
			}
		}()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		work <- struct{}{}
	}
	close(work)
	wg.Wait()
	if bErr != nil {
		b.Fatalf("IssueToken (concurrent): %v", bErr)
	}
}
