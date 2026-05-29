package interceptor

import (
	"context"

	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

const (
	grpcMethodIssueToken = "/token.v1.TokenEngine/IssueToken"

	idempotencyRedisPrefix     = "idempotency"
	idempotencyKeySep          = ":"
	idempotencyDefaultTenantID = "default"
	idempotencyMethodIssue     = "IssueToken"

	idempotencyResultHit   = "hit"
	idempotencyResultMiss  = "miss"
	idempotencyLabelResult = "result"
	idempotencyLabelMethod = "rpc_method"
)

// NewIdempotencyInterceptor returns a gRPC unary server interceptor that provides
// at-most-once semantics for IssueToken RPCs.
//
// Active for IssueToken only (grpcMethodIssueToken). All other methods pass through immediately.
// x-idempotency-key absent or empty: pass through without store interaction.
// Cache miss: call handler; on success, cache via SetNX; return response.
// Cache hit: unmarshal cached proto response; return without calling handler.
// Store errors (Get or SetNX): log at Warn and pass through — do not fail the RPC.
func NewIdempotencyInterceptor(st store.IdempotencyStore, logger observability.Logger, metrics observability.Metrics) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// ===== STEP 1: Method guard =====
		if info.FullMethod != grpcMethodIssueToken {
			return handler(ctx, req)
		}

		// ===== STEP 2: Extract x-idempotency-key =====
		var clientKey string
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if vals := md.Get(observability.MetadataKeyIdempotencyKey); len(vals) > 0 {
				clientKey = vals[0]
			}
		}
		if clientKey == "" {
			return handler(ctx, req)
		}

		// ===== STEP 3: Extract tenantID via type assertion =====
		issueReq, ok := req.(*tokenv1.IssueTokenRequest)
		if !ok {
			logger.Error(ctx, "idempotency interceptor: req type assertion to *IssueTokenRequest failed")
			return handler(ctx, req)
		}
		tenantID := issueReq.TenantId
		if tenantID == "" {
			tenantID = idempotencyDefaultTenantID
		}

		// ===== STEP 4: Construct Redis key =====
		key := idempotencyRedisPrefix + idempotencyKeySep + tenantID + idempotencyKeySep + idempotencyMethodIssue + idempotencyKeySep + clientKey

		// ===== STEP 5: Check cache =====
		missLabels := map[string]string{
			idempotencyLabelResult: idempotencyResultMiss,
			idempotencyLabelMethod: info.FullMethod,
		}

		cached, hit, getErr := st.Get(ctx, key)
		if getErr != nil {
			logger.Warn(ctx, "idempotency store Get error; treating as miss", "error", getErr)
		} else if hit {
			// ===== STEP 6: Cache hit — unmarshal and return =====
			var resp tokenv1.TokenPair
			if unmarshalErr := proto.Unmarshal(cached, &resp); unmarshalErr != nil {
				logger.Warn(ctx, "idempotency cached bytes unmarshal failed; treating as miss", "error", unmarshalErr)
			} else {
				hitLabels := map[string]string{
					idempotencyLabelResult: idempotencyResultHit,
					idempotencyLabelMethod: info.FullMethod,
				}
				metrics.IncrementCounter(observability.MetricIdempotencyTotal, hitLabels)
				return &resp, nil
			}
		}

		// ===== STEP 7: Cache miss — call handler =====
		resp, handlerErr := handler(ctx, req)
		if handlerErr != nil {
			metrics.IncrementCounter(observability.MetricIdempotencyTotal, missLabels)
			return nil, handlerErr
		}

		// ===== STEP 8: Cache response on handler success =====
		if issueResp, ok := resp.(*tokenv1.TokenPair); ok {
			if marshaledBytes, marshalErr := proto.Marshal(issueResp); marshalErr == nil {
				if _, setErr := st.SetNX(ctx, key, marshaledBytes); setErr != nil {
					logger.Warn(ctx, "idempotency store SetNX error", "error", setErr)
				}
				// SetNX returning (false, nil) is a concurrent write — no log, no error
			}
		}

		metrics.IncrementCounter(observability.MetricIdempotencyTotal, missLabels)
		return resp, nil
	}
}
