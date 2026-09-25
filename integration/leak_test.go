package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/alicebob/miniredis/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"github.com/aetomala/token-engine/internal/audit"
	"github.com/aetomala/token-engine/internal/config"
	"github.com/aetomala/token-engine/internal/handler"
	"github.com/aetomala/token-engine/internal/interceptor"
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/registry"
	"github.com/aetomala/token-engine/internal/store"
)

// leakWindow is the fragment length used to detect partial credential leaks.
const leakWindow = 12

// syncBuffer is a goroutine-safe bytes.Buffer used as the SlogLogger sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends p to the buffer. Safe for concurrent use.
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns a snapshot of the buffer contents. Safe for concurrent use.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// secretTracker records credentials and detects any 12-character fragment of them in recorded output.
type secretTracker struct {
	mu        sync.Mutex
	seen      map[string]struct{} // secret -> tracked
	windows   map[string]string   // 12-character fragment -> owning secret
	shortFrag map[string]string   // fragment shorter than 12 characters -> owning secret
}

// newSecretTracker returns an empty secretTracker.
func newSecretTracker() *secretTracker {
	return &secretTracker{
		seen:      make(map[string]struct{}),
		windows:   make(map[string]string),
		shortFrag: make(map[string]string),
	}
}

// track registers secret and builds its fragment set once. A JWT — exactly three dot-separated
// segments — contributes only its payload and signature segments, since the header is shared by
// every token signed with the same key. Empty secrets are ignored.
func (t *secretTracker) track(secret string) {
	if secret == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.seen[secret]; ok {
		return
	}
	t.seen[secret] = struct{}{}

	parts := []string{secret}
	if segs := strings.Split(secret, "."); len(segs) == 3 {
		parts = segs[1:]
	}
	for _, p := range parts {
		if len(p) < leakWindow {
			if p != "" {
				t.shortFrag[p] = secret
			}
			continue
		}
		for i := 0; i+leakWindow <= len(p); i++ {
			t.windows[p[i:i+leakWindow]] = secret
		}
	}
}

// find returns the first tracked fragment contained in recorded and its owning secret.
func (t *secretTracker) find(recorded string) (fragment, secret string, found bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := 0; i+leakWindow <= len(recorded); i++ {
		if s, ok := t.windows[recorded[i:i+leakWindow]]; ok {
			return recorded[i : i+leakWindow], s, true
		}
	}
	for frag, s := range t.shortFrag {
		if strings.Contains(recorded, frag) {
			return frag, s, true
		}
	}
	return "", "", false
}

// errorRecorder collects the status message of every gRPC error returned to the client.
type errorRecorder struct {
	mu       sync.Mutex
	messages []string
}

// interceptor returns a client interceptor that records non-nil error messages.
func (r *errorRecorder) interceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			r.mu.Lock()
			r.messages = append(r.messages, status.Convert(err).Message())
			r.mu.Unlock()
		}
		return err
	}
}

// snapshot returns a copy of the recorded error messages.
func (r *errorRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.messages...)
}

// spanStrings flattens every recordable string of a span — name, attributes, events, status.
func spanStrings(s tracetest.SpanStub) []string {
	out := []string{"span name: " + s.Name}
	for _, kv := range s.Attributes {
		out = append(out, fmt.Sprintf("span %q attribute %s=%s", s.Name, kv.Key, kv.Value.Emit()))
	}
	for _, ev := range s.Events {
		out = append(out, fmt.Sprintf("span %q event name: %s", s.Name, ev.Name))
		for _, kv := range ev.Attributes {
			out = append(out, fmt.Sprintf("span %q event %q attribute %s=%s", s.Name, ev.Name, kv.Key, kv.Value.Emit()))
		}
	}
	out = append(out, fmt.Sprintf("span %q status description: %s", s.Name, s.Status.Description))
	return out
}

// randomGarbageToken returns a random URL-safe string shaped like an opaque refresh token.
func randomGarbageToken() string {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	Expect(err).NotTo(HaveOccurred())
	return base64.RawURLEncoding.EncodeToString(b)
}

var _ = Describe("Credential leak regression", Ordered, func() {
	const (
		leakTenant = "leak-issuer"
		leakAPIKey = "leak-api-key"
	)

	var (
		leakMR       *miniredis.Miniredis
		leakServer   *grpc.Server
		leakConn     *grpc.ClientConn
		leakClient   tokenv1.TokenEngineClient
		leakKM       interface{ Shutdown(context.Context) error }
		leakTP       *sdktrace.TracerProvider
		spanExporter *tracetest.InMemoryExporter
		logBuf       *syncBuffer
		secrets      *secretTracker
		grpcErrors   *errorRecorder
	)

	// leakCtx returns a context carrying the leak-suite API key and optional extra metadata pairs.
	leakCtx := func(extra ...string) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ctx = metadata.AppendToOutgoingContext(ctx, append([]string{"x-api-key", leakAPIKey}, extra...)...)
		return ctx, cancel
	}

	// trackPair registers both tokens of a pair as secrets.
	trackPair := func(p *tokenv1.TokenPair) {
		if p == nil {
			return
		}
		secrets.track(p.AccessToken)
		secrets.track(p.RefreshToken)
	}

	// issue issues a token pair for sub, tracks it, and returns it.
	issue := func(sub string) *tokenv1.TokenPair {
		ctx, cancel := leakCtx()
		defer cancel()
		resp, err := leakClient.IssueToken(ctx, &tokenv1.IssueTokenRequest{Sub: sub, TenantId: leakTenant})
		trackPair(resp)
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	BeforeAll(func() {
		var err error

		// ===== STEP 1: Start miniredis =====
		leakMR, err = miniredis.Run()
		Expect(err).NotTo(HaveOccurred())

		cfg := &config.Config{
			Issuer:           leakTenant,
			Audience:         "api",
			TLSMode:          "disabled",
			StaticCallerKeys: map[string]string{leakAPIKey: "leak-caller"},
			RedisAddr:        leakMR.Addr(),
			IdempotencyTTL:   24 * time.Hour,
			LockTTL:          30 * time.Second,
			JWKSCacheMaxAge:  5 * time.Minute,
		}

		// ===== STEP 2: Recording observability — JSON logs, in-memory spans =====
		logBuf = &syncBuffer{}
		secrets = newSecretTracker()
		grpcErrors = &errorRecorder{}
		logger := observability.NewSlogLogger(logBuf)
		spanExporter = tracetest.NewInMemoryExporter()
		leakTP = sdktrace.NewTracerProvider(sdktrace.WithSyncer(spanExporter))
		tracer := observability.NewOtelTracer(leakTP.Tracer("token-engine"))
		promReg := prometheus.NewRegistry()
		metrics := observability.NewPrometheusMetrics(promReg)

		// ===== STEP 3: Tenant registry — same constructor arguments as main.go =====
		redisClient := redis.NewClient(&redis.Options{Addr: leakMR.Addr()})
		initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer initCancel()
		tenantReg := registry.NewMultiTenantRegistry(redisClient, promReg, logger, tracer, metrics)
		Expect(tenantReg.Add(initCtx, cfg.Issuer, registry.TenantConfig{
			Issuer:   cfg.Issuer,
			Audience: cfg.Audience,
		})).To(Succeed())
		leakKM = tenantReg.AllKeyManagers()[cfg.Issuer]

		// ===== STEP 4: Interceptors — same order as main.go =====
		auth := interceptor.NewStaticKeyAuthenticator(cfg.StaticCallerKeys)
		callerReg := registry.NewNoOpCallerRegistry()
		idempStore := store.NewRedisIdempotencyStore(redisClient, cfg.IdempotencyTTL, cfg.LockTTL)
		auditStore := audit.NewSlogAuditStore(logger)

		leakServer = grpc.NewServer(
			grpc.ChainUnaryInterceptor(
				otelgrpc.UnaryServerInterceptor(otelgrpc.WithTracerProvider(leakTP)), //nolint:staticcheck // mirrors main.go; v0.52.0 pinned
				observability.NewCorrelationInterceptor(logger, metrics),
				interceptor.NewAuthInterceptor(auth, logger),
				interceptor.NewCallerAuthorizationInterceptor(callerReg, logger),
				interceptor.NewIdempotencyInterceptor(idempStore, logger, metrics),
				interceptor.NewValidationInterceptor(logger),
			),
		)
		tokenv1.RegisterTokenEngineServer(leakServer, handler.NewTokenHandler(tenantReg, auditStore, logger, tracer, metrics))

		// ===== STEP 5: Serve and connect =====
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		go func() { _ = leakServer.Serve(listener) }()

		leakConn, err = grpc.NewClient(
			listener.Addr().String(),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithUnaryInterceptor(grpcErrors.interceptor()),
		)
		Expect(err).NotTo(HaveOccurred())
		leakClient = tokenv1.NewTokenEngineClient(leakConn)
	})

	AfterAll(func() {
		if leakConn != nil {
			_ = leakConn.Close()
		}
		if leakServer != nil {
			leakServer.GracefulStop()
		}
		if leakKM != nil {
			_ = leakKM.Shutdown(context.Background())
		}
		if leakTP != nil {
			_ = leakTP.Shutdown(context.Background())
		}
		if leakMR != nil {
			leakMR.Close()
		}
	})

	AfterEach(func() {
		// ===== STEP 1: Log stream =====
		for _, line := range strings.Split(logBuf.String(), "\n") {
			if frag, secret, found := secrets.find(line); found {
				Fail(fmt.Sprintf("log line leaks a credential fragment\n  fragment: %q\n  secret:   %q\n  recorded: %s", frag, secret, line))
			}
		}

		// ===== STEP 2: Exported spans =====
		for _, s := range spanExporter.GetSpans() {
			for _, recorded := range spanStrings(s) {
				if frag, secret, found := secrets.find(recorded); found {
					Fail(fmt.Sprintf("exported span leaks a credential fragment\n  fragment: %q\n  secret:   %q\n  recorded: %s", frag, secret, recorded))
				}
			}
		}

		// ===== STEP 3: gRPC error messages returned to the client =====
		for _, msg := range grpcErrors.snapshot() {
			if frag, secret, found := secrets.find(msg); found {
				Fail(fmt.Sprintf("gRPC error message leaks a credential fragment\n  fragment: %q\n  secret:   %q\n  recorded: %s", frag, secret, msg))
			}
		}
	})

	// ===== PHASE 3: IssueToken =====
	Describe("Phase 3: IssueToken", func() {
		It("does not leak tokens issued with audiences and custom claims", func() {
			ctx, cancel := leakCtx()
			defer cancel()

			resp, err := leakClient.IssueToken(ctx, &tokenv1.IssueTokenRequest{
				Sub:       "leak-user-claims",
				TenantId:  leakTenant,
				Audiences: []string{"api", "admin"},
				Claims:    map[string]string{"role": "admin", "org": "acme"},
			})
			trackPair(resp)

			Expect(err).NotTo(HaveOccurred())
			Expect(resp.RefreshToken).NotTo(BeEmpty())
		})

		It("does not leak tokens issued or replayed under an x-idempotency-key", func() {
			// ===== STEP 1: First call =====
			ctx1, cancel1 := leakCtx(observability.MetadataKeyIdempotencyKey, "leak-idem-key-1")
			defer cancel1()
			first, err := leakClient.IssueToken(ctx1, &tokenv1.IssueTokenRequest{Sub: "leak-user-idem", TenantId: leakTenant})
			trackPair(first)
			Expect(err).NotTo(HaveOccurred())

			// ===== STEP 2: Retry with the same key =====
			ctx2, cancel2 := leakCtx(observability.MetadataKeyIdempotencyKey, "leak-idem-key-1")
			defer cancel2()
			second, err := leakClient.IssueToken(ctx2, &tokenv1.IssueTokenRequest{Sub: "leak-user-idem", TenantId: leakTenant})
			trackPair(second)
			Expect(err).NotTo(HaveOccurred())
			Expect(second.AccessToken).To(Equal(first.AccessToken))
		})
	})

	// ===== PHASE 3: RefreshToken =====
	Describe("Phase 3: RefreshToken", func() {
		It("does not leak the presented or rotated tokens on success", func() {
			issued := issue("leak-user-refresh")

			ctx, cancel := leakCtx()
			defer cancel()
			rotated, err := leakClient.RefreshToken(ctx, &tokenv1.RefreshTokenRequest{
				RefreshToken: issued.RefreshToken,
				TenantId:     leakTenant,
			})
			trackPair(rotated)

			Expect(err).NotTo(HaveOccurred())
			Expect(rotated.RefreshToken).NotTo(Equal(issued.RefreshToken))
		})

		It("does not leak a rotated-out refresh token presented again", func() {
			// ===== STEP 1: Issue and rotate =====
			issued := issue("leak-user-reuse")
			ctx1, cancel1 := leakCtx()
			defer cancel1()
			rotated, err := leakClient.RefreshToken(ctx1, &tokenv1.RefreshTokenRequest{
				RefreshToken: issued.RefreshToken,
				TenantId:     leakTenant,
			})
			trackPair(rotated)
			Expect(err).NotTo(HaveOccurred())

			// ===== STEP 2: Present the revoked token =====
			ctx2, cancel2 := leakCtx()
			defer cancel2()
			reused, err := leakClient.RefreshToken(ctx2, &tokenv1.RefreshTokenRequest{
				RefreshToken: issued.RefreshToken,
				TenantId:     leakTenant,
			})
			trackPair(reused)
			Expect(err).To(HaveOccurred())
		})

		It("does not leak a random garbage token", func() {
			garbage := randomGarbageToken()
			secrets.track(garbage)

			ctx, cancel := leakCtx()
			defer cancel()
			resp, err := leakClient.RefreshToken(ctx, &tokenv1.RefreshTokenRequest{
				RefreshToken: garbage,
				TenantId:     leakTenant,
			})
			trackPair(resp)

			Expect(err).To(HaveOccurred())
		})
	})

	// ===== PHASE 3: Revocation =====
	Describe("Phase 3: Revocation", func() {
		It("RevokeToken does not leak the revoked refresh token", func() {
			issued := issue("leak-user-revoke")

			ctx, cancel := leakCtx()
			defer cancel()
			_, err := leakClient.RevokeToken(ctx, &tokenv1.RevokeTokenRequest{
				RefreshToken: issued.RefreshToken,
				TenantId:     leakTenant,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Contains(logBuf.String(), `"token_ref"`)).To(BeTrue(), "audit record should carry token_ref")
		})

		It("RevokeAllUserTokens does not leak the user's tokens", func() {
			issue("leak-user-revoke-all")
			issue("leak-user-revoke-all")

			ctx, cancel := leakCtx()
			defer cancel()
			_, err := leakClient.RevokeAllUserTokens(ctx, &tokenv1.RevokeUserRequest{
				UserId:   "leak-user-revoke-all",
				TenantId: leakTenant,
			})

			Expect(err).NotTo(HaveOccurred())
		})

		It("RevokeAllForAudience does not leak the audience's tokens", func() {
			issue("leak-user-revoke-aud")

			ctx, cancel := leakCtx()
			defer cancel()
			_, err := leakClient.RevokeAllForAudience(ctx, &tokenv1.RevokeAudienceRequest{
				Audience: "api",
				TenantId: leakTenant,
			})

			Expect(err).NotTo(HaveOccurred())
		})

		It("RevokeAllForUserAndAudience does not leak the user's tokens", func() {
			issue("leak-user-revoke-user-aud")

			ctx, cancel := leakCtx()
			defer cancel()
			_, err := leakClient.RevokeAllForUserAndAudience(ctx, &tokenv1.RevokeUserAndAudienceRequest{
				UserId:   "leak-user-revoke-user-aud",
				Audience: "api",
				TenantId: leakTenant,
			})

			Expect(err).NotTo(HaveOccurred())
		})
	})
})
