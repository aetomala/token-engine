package interceptor

import (
	"context"
	"encoding/json"

	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	grpcMethodIssueToken   = "/token.v1.TokenEngine/IssueToken"
	grpcMethodRefreshToken = "/token.v1.TokenEngine/RefreshToken"

	idempotencyRedisPrefix     = "idempotency"
	idempotencyKeySep          = ":"
	idempotencyDefaultTenantID = "default"
	idempotencyMethodIssue     = "IssueToken"
	idempotencyMethodRefresh   = "RefreshToken"

	idempotencyResultHit   = "hit"
	idempotencyResultMiss  = "miss"
	idempotencyLabelResult = "result"
	idempotencyLabelMethod = "rpc_method"

	idempotencyAbortedMsg = "a request with this idempotency key is already in progress; retry"
)

// idempotencyRecordMagic prefixes every record written in the versioned envelope format. Bytes
// without this prefix are a legacy bare-marshaled-TokenPair record, written before this format
// existed — preserved for backward compatibility until they expire from their original TTL.
var idempotencyRecordMagic = []byte("IDR1")

// idempotencyRecordState is the lifecycle state of a stored idempotency record.
type idempotencyRecordState string

const (
	idempotencyStatePending   idempotencyRecordState = "pending"
	idempotencyStateCompleted idempotencyRecordState = "completed"
)

// idempotencyRecord is the versioned envelope stored at an idempotency key. Response is
// present only when State is idempotencyStateCompleted — it holds a marshaled *tokenv1.TokenPair.
// Future issues extend this envelope (a request-content fingerprint, a token reference) rather
// than introducing a separate record format — see ADR-012.
type idempotencyRecord struct {
	State    idempotencyRecordState `json:"state"`
	Response []byte                 `json:"response,omitempty"`
}

// idempotencyPendingRecord is the fixed byte value SetNX writes to claim a key before its
// handler runs.
var idempotencyPendingRecord = append(append([]byte{}, idempotencyRecordMagic...), []byte(`{"state":"pending"}`)...)

// encodeCompletedRecord marshals a completed idempotencyRecord wrapping response into the
// magic-prefixed versioned envelope format. Returns an error if JSON marshaling fails.
func encodeCompletedRecord(response []byte) ([]byte, error) {
	body, err := json.Marshal(idempotencyRecord{State: idempotencyStateCompleted, Response: response})
	if err != nil {
		return nil, err
	}
	return append(append([]byte{}, idempotencyRecordMagic...), body...), nil
}

// decodeIdempotencyRecord unmarshals raw into an idempotencyRecord. Returns ok=false when raw
// lacks the magic prefix or fails to unmarshal — the caller falls back to treating raw as a
// legacy bare-TokenPair record.
func decodeIdempotencyRecord(raw []byte) (rec idempotencyRecord, ok bool) {
	if len(raw) < len(idempotencyRecordMagic) || string(raw[:len(idempotencyRecordMagic)]) != string(idempotencyRecordMagic) {
		return idempotencyRecord{}, false
	}
	if err := json.Unmarshal(raw[len(idempotencyRecordMagic):], &rec); err != nil {
		return idempotencyRecord{}, false
	}
	return rec, true
}

// resolveExistingRecord inspects bytes read from the store after a failed claim (SetNX
// returned false). Returns (resp, true, nil) if a completed response — including a legacy
// bare-TokenPair record predating the versioned envelope — was found. Returns (nil, false, nil)
// if the record is still pending — the caller fails fast with codes.Aborted. Returns
// (nil, false, err) if a completed record's response, or legacy bytes, failed to unmarshal.
func resolveExistingRecord(raw []byte) (*tokenv1.TokenPair, bool, error) {
	if rec, ok := decodeIdempotencyRecord(raw); ok {
		if rec.State == idempotencyStatePending {
			return nil, false, nil
		}
		var resp tokenv1.TokenPair
		if err := proto.Unmarshal(rec.Response, &resp); err != nil {
			return nil, false, err
		}
		return &resp, true, nil
	}

	// Legacy bare-TokenPair bytes, predating the versioned envelope.
	var resp tokenv1.TokenPair
	if err := proto.Unmarshal(raw, &resp); err != nil {
		return nil, false, err
	}
	return &resp, true, nil
}

// PendingRecordForTest returns the fixed byte value SetNX writes to claim a key, for testing
// purposes only.
func PendingRecordForTest() []byte {
	return idempotencyPendingRecord
}

// EncodeCompletedRecordForTest returns the magic-prefixed versioned envelope wrapping response,
// for testing purposes only.
func EncodeCompletedRecordForTest(response []byte) []byte {
	b, err := encodeCompletedRecord(response)
	if err != nil {
		panic(err)
	}
	return b
}

// ResolveExistingRecordForTest exposes resolveExistingRecord, for testing purposes only.
func ResolveExistingRecordForTest(raw []byte) (*tokenv1.TokenPair, bool, error) {
	return resolveExistingRecord(raw)
}

// NewIdempotencyInterceptor returns a gRPC unary server interceptor providing at-most-once
// semantics for IssueToken and RefreshToken RPCs, including for requests that arrive
// concurrently with the same idempotency key.
//
// Ordering: the interceptor claims the idempotency key atomically before the handler runs. A
// request that wins the claim proceeds to the handler; on success the claim is promoted to a
// completed record holding the response. A request that loses the claim either receives the
// cached response (the key already holds a completed record — the normal sequential-retry
// case) or, if the key holds another request's still-pending claim, is rejected immediately
// with codes.Aborted rather than blocking or racing the in-flight request. See ADR-012 for the
// full design, including why concurrent duplicates fail fast instead of waiting.
//
// CRITICAL ordering for RefreshToken: the idempotency claim MUST occur BEFORE the library call.
// As of jwtauth v0.6.0 (#195), RefreshAccessTokenWithClaims revokes the old refresh token
// immediately. A retry arriving after the first call will receive ErrTokenRevoked from the
// library — the cached response must be returned before the library is ever called.
//
// Key construction: idempotency:{tenantID}:{method}:{clientKey}
// where method is "IssueToken" or "RefreshToken".
//
// Response type for both methods: *tokenv1.TokenPair.
// X-idempotency-key absent or empty: pass through without store interaction.
func NewIdempotencyInterceptor(st store.IdempotencyStore, logger observability.Logger, metrics observability.Metrics) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// ===== STEP 1: Method guard =====
		if info.FullMethod != grpcMethodIssueToken && info.FullMethod != grpcMethodRefreshToken {
			return handler(ctx, req)
		}

		if info.FullMethod == grpcMethodIssueToken {
			return handleIssueTokenIdempotency(ctx, req, info, handler, st, logger, metrics)
		}
		return handleRefreshTokenIdempotency(ctx, req, info, handler, st, logger, metrics)
	}
}

func handleIssueTokenIdempotency(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler, st store.IdempotencyStore, logger observability.Logger, metrics observability.Metrics) (interface{}, error) {
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

	missLabels := map[string]string{
		idempotencyLabelResult: idempotencyResultMiss,
		idempotencyLabelMethod: info.FullMethod,
	}
	hitLabels := map[string]string{
		idempotencyLabelResult: idempotencyResultHit,
		idempotencyLabelMethod: info.FullMethod,
	}

	// ===== STEP 5: Claim the key before calling the handler =====
	claimed, claimErr := st.SetNX(ctx, key, idempotencyPendingRecord)
	if claimErr != nil {
		logger.Warn(ctx, "idempotency store SetNX (claim) error; proceeding without concurrent-claim protection", "error", claimErr)
		claimed = true
	}

	if !claimed {
		// ===== STEP 6: Lost the claim — inspect the existing record =====
		cached, hit, getErr := st.Get(ctx, key)
		if getErr != nil {
			logger.Warn(ctx, "idempotency store Get error after failed claim; treating as concurrent duplicate", "error", getErr)
			return nil, status.Error(codes.Aborted, idempotencyAbortedMsg)
		}
		if hit {
			resp, isCompleted, resolveErr := resolveExistingRecord(cached)
			if resolveErr != nil {
				logger.Warn(ctx, "idempotency cached record unreadable; treating as concurrent duplicate", "error", resolveErr)
			} else if isCompleted {
				metrics.IncrementCounter(observability.MetricIdempotencyTotal, hitLabels)
				return resp, nil
			}
		}
		return nil, status.Error(codes.Aborted, idempotencyAbortedMsg)
	}

	// ===== STEP 7: Won the claim — call handler =====
	resp, handlerErr := handler(ctx, req)
	if handlerErr != nil {
		metrics.IncrementCounter(observability.MetricIdempotencyTotal, missLabels)
		return nil, handlerErr
	}

	// ===== STEP 8: Promote the claim to a completed record =====
	if issueResp, ok := resp.(*tokenv1.TokenPair); ok {
		if marshaledBytes, marshalErr := proto.Marshal(issueResp); marshalErr == nil {
			if recordBytes, encodeErr := encodeCompletedRecord(marshaledBytes); encodeErr == nil {
				if setErr := st.Set(ctx, key, recordBytes); setErr != nil {
					logger.Warn(ctx, "idempotency store Set (promote) error", "error", setErr)
				}
			}
		}
	}

	metrics.IncrementCounter(observability.MetricIdempotencyTotal, missLabels)
	return resp, nil
}

func handleRefreshTokenIdempotency(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler, st store.IdempotencyStore, logger observability.Logger, metrics observability.Metrics) (interface{}, error) {
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
	refreshReq, ok := req.(*tokenv1.RefreshTokenRequest)
	if !ok {
		logger.Error(ctx, "idempotency interceptor: req type assertion to *RefreshTokenRequest failed")
		return handler(ctx, req)
	}
	tenantID := refreshReq.TenantId
	if tenantID == "" {
		tenantID = idempotencyDefaultTenantID
	}

	// ===== STEP 4: Construct Redis key =====
	key := idempotencyRedisPrefix + idempotencyKeySep + tenantID + idempotencyKeySep + idempotencyMethodRefresh + idempotencyKeySep + clientKey

	missLabels := map[string]string{
		idempotencyLabelResult: idempotencyResultMiss,
		idempotencyLabelMethod: info.FullMethod,
	}
	hitLabels := map[string]string{
		idempotencyLabelResult: idempotencyResultHit,
		idempotencyLabelMethod: info.FullMethod,
	}

	// ===== STEP 5: Claim the key before calling the handler (ordering constraint) =====
	// CRITICAL: the claim MUST precede the handler. jwtauth v0.6.0 (#195) revokes the old
	// refresh token immediately — a retry after the first call receives ErrTokenRevoked.
	claimed, claimErr := st.SetNX(ctx, key, idempotencyPendingRecord)
	if claimErr != nil {
		logger.Warn(ctx, "idempotency store SetNX (claim) error; proceeding without concurrent-claim protection", "error", claimErr)
		claimed = true
	}

	if !claimed {
		// ===== STEP 6: Lost the claim — inspect the existing record, without calling the handler =====
		cached, hit, getErr := st.Get(ctx, key)
		if getErr != nil {
			logger.Warn(ctx, "idempotency store Get error after failed claim; treating as concurrent duplicate", "error", getErr)
			return nil, status.Error(codes.Aborted, idempotencyAbortedMsg)
		}
		if hit {
			resp, isCompleted, resolveErr := resolveExistingRecord(cached)
			if resolveErr != nil {
				logger.Warn(ctx, "idempotency cached record unreadable; treating as concurrent duplicate", "error", resolveErr)
			} else if isCompleted {
				metrics.IncrementCounter(observability.MetricIdempotencyTotal, hitLabels)
				return resp, nil
			}
		}
		return nil, status.Error(codes.Aborted, idempotencyAbortedMsg)
	}

	// ===== STEP 7: Won the claim — call handler =====
	resp, handlerErr := handler(ctx, req)
	if handlerErr != nil {
		metrics.IncrementCounter(observability.MetricIdempotencyTotal, missLabels)
		return nil, handlerErr
	}

	// ===== STEP 8: Promote the claim to a completed record =====
	if refreshResp, ok := resp.(*tokenv1.TokenPair); ok {
		if marshaledBytes, marshalErr := proto.Marshal(refreshResp); marshalErr == nil {
			if recordBytes, encodeErr := encodeCompletedRecord(marshaledBytes); encodeErr == nil {
				if setErr := st.Set(ctx, key, recordBytes); setErr != nil {
					logger.Warn(ctx, "idempotency store Set (promote) error", "error", setErr)
				}
			}
		}
	}

	metrics.IncrementCounter(observability.MetricIdempotencyTotal, missLabels)
	return resp, nil
}
