package interceptor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

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

	idempotencyResultHit      = "hit"
	idempotencyResultMiss     = "miss"
	idempotencyResultMismatch = "mismatch"
	idempotencyLabelResult    = "result"
	idempotencyLabelMethod    = "rpc_method"

	idempotencyAbortedMsg             = "a request with this idempotency key is already in progress; retry"
	idempotencyFingerprintMismatchMsg = "this idempotency key was previously used with different request content; use a new key"
	idempotencyKeyConflictMsg         = "idempotency_key request field and x-idempotency-key metadata header are both set but differ; set only one"
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

// idempotencyRecord is the versioned envelope stored at an idempotency key. Response and
// Fingerprint are present only when State is idempotencyStateCompleted — Response holds a
// marshaled *tokenv1.TokenPair, Fingerprint holds the hex-encoded SHA-256 hash of the request
// content that produced it (see computeFingerprint). Fingerprint is empty for records written
// before it existed (#127-era and legacy bare-TokenPair records) — resolveExistingRecord skips
// the mismatch check for those, see ADR-013. Future issues extend this envelope (e.g. #117's
// token reference) rather than introducing a separate record format — see ADR-012.
type idempotencyRecord struct {
	State       idempotencyRecordState `json:"state"`
	Response    []byte                 `json:"response,omitempty"`
	Fingerprint string                 `json:"fingerprint,omitempty"`
}

// idempotencyPendingRecord is the fixed byte value SetNX writes to claim a key before its
// handler runs.
var idempotencyPendingRecord = append(append([]byte{}, idempotencyRecordMagic...), []byte(`{"state":"pending"}`)...)

// encodeCompletedRecord marshals a completed idempotencyRecord wrapping response and its
// request-content fingerprint into the magic-prefixed versioned envelope format. Returns an
// error if JSON marshaling fails.
func encodeCompletedRecord(response []byte, fingerprint string) ([]byte, error) {
	body, err := json.Marshal(idempotencyRecord{State: idempotencyStateCompleted, Response: response, Fingerprint: fingerprint})
	if err != nil {
		return nil, err
	}
	return append(append([]byte{}, idempotencyRecordMagic...), body...), nil
}

// computeFingerprint returns the hex-encoded SHA-256 hash of v's canonical JSON encoding, for
// use as an idempotencyRecord's Fingerprint. Callers pass a struct built from exactly the request
// fields that determine the result — map fields marshal with keys sorted alphabetically by
// encoding/json, giving a deterministic digest regardless of map iteration order. Returns "" if
// marshaling fails; callers treat that as "fingerprint unavailable" and skip the mismatch check
// for the record being written, degrading open rather than rejecting the request.
func computeFingerprint(v interface{}) string {
	body, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// issueFingerprintInput is the canonical, hashable projection of an IssueTokenRequest's
// result-determining fields — everything except IdempotencyKey.
type issueFingerprintInput struct {
	Sub       string            `json:"sub"`
	TenantID  string            `json:"tenant_id"`
	Claims    map[string]string `json:"claims"`
	Audiences []string          `json:"audiences"`
}

// computeIssueFingerprint returns the fingerprint of req's result-determining fields (subject,
// tenant, claims, audiences). Audiences are sorted before hashing so equivalent requests with
// differently ordered audiences produce the same fingerprint.
func computeIssueFingerprint(req *tokenv1.IssueTokenRequest) string {
	audiences := append([]string{}, req.Audiences...)
	sort.Strings(audiences)
	return computeFingerprint(issueFingerprintInput{
		Sub:       req.Sub,
		TenantID:  req.TenantId,
		Claims:    req.Claims,
		Audiences: audiences,
	})
}

// refreshFingerprintInput is the canonical, hashable projection of a RefreshTokenRequest's
// result-determining fields — everything except IdempotencyKey. RefreshToken is included so the
// hash binds to it, but only this struct's SHA-256 digest is ever persisted — the raw token
// itself is never written to the store.
type refreshFingerprintInput struct {
	RefreshToken string            `json:"refresh_token"`
	TenantID     string            `json:"tenant_id"`
	Claims       map[string]string `json:"claims"`
}

// computeRefreshFingerprint returns the fingerprint of req's result-determining fields (refresh
// token, tenant, claims). See refreshFingerprintInput for the no-raw-credential-at-rest guarantee.
func computeRefreshFingerprint(req *tokenv1.RefreshTokenRequest) string {
	return computeFingerprint(refreshFingerprintInput{
		RefreshToken: req.RefreshToken,
		TenantID:     req.TenantId,
		Claims:       req.Claims,
	})
}

// resolveIdempotencyKey resolves the effective idempotency key from the x-idempotency-key
// metadata header and the idempotency_key request field, per ADR-014. Comparison is exact-string
// equality — no trimming or case-folding. Returns ("", nil) when neither is set — the caller
// passes through without store interaction. Returns the single supplied value when only one is
// set, or either value when both are set and equal. Returns ("", err) with a codes.InvalidArgument
// error when both are set and differ — the caller must reject the request before any store
// interaction, without attempting a claim or calling the handler.
func resolveIdempotencyKey(headerKey, fieldKey string) (string, error) {
	switch {
	case headerKey == "":
		return fieldKey, nil
	case fieldKey == "", headerKey == fieldKey:
		return headerKey, nil
	default:
		return "", status.Error(codes.InvalidArgument, idempotencyKeyConflictMsg)
	}
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
// returned false). Returns (resp, fingerprint, true, nil) if a completed response — including a
// legacy bare-TokenPair record predating the versioned envelope — was found; fingerprint is ""
// for records written before it existed (legacy records and #127-era records), which the caller
// treats as "no mismatch check possible" rather than a mismatch. Returns (nil, "", false, nil) if
// the record is still pending — the caller fails fast with codes.Aborted. Returns
// (nil, "", false, err) if a completed record's response, or legacy bytes, failed to unmarshal.
func resolveExistingRecord(raw []byte) (*tokenv1.TokenPair, string, bool, error) {
	if rec, ok := decodeIdempotencyRecord(raw); ok {
		if rec.State == idempotencyStatePending {
			return nil, "", false, nil
		}
		var resp tokenv1.TokenPair
		if err := proto.Unmarshal(rec.Response, &resp); err != nil {
			return nil, "", false, err
		}
		return &resp, rec.Fingerprint, true, nil
	}

	// Legacy bare-TokenPair bytes, predating the versioned envelope.
	var resp tokenv1.TokenPair
	if err := proto.Unmarshal(raw, &resp); err != nil {
		return nil, "", false, err
	}
	return &resp, "", true, nil
}

// PendingRecordForTest returns the fixed byte value SetNX writes to claim a key, for testing
// purposes only.
func PendingRecordForTest() []byte {
	return idempotencyPendingRecord
}

// EncodeCompletedRecordForTest returns the magic-prefixed versioned envelope wrapping response
// and fingerprint, for testing purposes only.
func EncodeCompletedRecordForTest(response []byte, fingerprint string) []byte {
	b, err := encodeCompletedRecord(response, fingerprint)
	if err != nil {
		panic(err)
	}
	return b
}

// ComputeIssueFingerprintForTest exposes computeIssueFingerprint, for testing purposes only.
func ComputeIssueFingerprintForTest(req *tokenv1.IssueTokenRequest) string {
	return computeIssueFingerprint(req)
}

// ComputeRefreshFingerprintForTest exposes computeRefreshFingerprint, for testing purposes only.
func ComputeRefreshFingerprintForTest(req *tokenv1.RefreshTokenRequest) string {
	return computeRefreshFingerprint(req)
}

// ResolveExistingRecordForTest exposes resolveExistingRecord, for testing purposes only.
func ResolveExistingRecordForTest(raw []byte) (*tokenv1.TokenPair, string, bool, error) {
	return resolveExistingRecord(raw)
}

// ResolveIdempotencyKeyForTest exposes resolveIdempotencyKey, for testing purposes only.
func ResolveIdempotencyKeyForTest(headerKey, fieldKey string) (string, error) {
	return resolveIdempotencyKey(headerKey, fieldKey)
}

// NewIdempotencyInterceptor returns a gRPC unary server interceptor providing at-most-once
// semantics for IssueToken and RefreshToken RPCs, including for requests that arrive
// concurrently with the same idempotency key.
//
// Ordering: the interceptor claims the idempotency key atomically before the handler runs. A
// request that wins the claim proceeds to the handler; on success the claim is promoted to a
// completed record holding the response and a fingerprint of the request content that produced
// it. A request that loses the claim either receives the cached response (the key already holds
// a completed record whose fingerprint matches this request's own — the normal sequential-retry
// case), is rejected with codes.FailedPrecondition (the key holds a completed record whose
// fingerprint does not match — the key was reused with different request content, see ADR-013),
// or, if the key holds another request's still-pending claim, is rejected immediately with
// codes.Aborted rather than blocking or racing the in-flight request. See ADR-012 for the full
// design, including why concurrent duplicates fail fast instead of waiting.
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
//
// Client key source: the x-idempotency-key metadata header, or the idempotency_key request
// field as a fallback when the header is absent. When both are set and differ, the request is
// rejected with codes.InvalidArgument before any store interaction — neither value is preferred
// over the other, since the conflict itself indicates a caller-side integration bug. When both
// are set and equal, or only one is set, that value is used. See ADR-014 for the full precedence
// rule and rationale. Neither header nor field set: pass through without store interaction.
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
	// ===== STEP 2: Extract tenantID via type assertion =====
	issueReq, ok := req.(*tokenv1.IssueTokenRequest)
	if !ok {
		logger.Error(ctx, "idempotency interceptor: req type assertion to *IssueTokenRequest failed")
		return handler(ctx, req)
	}
	tenantID := issueReq.TenantId
	if tenantID == "" {
		tenantID = idempotencyDefaultTenantID
	}

	// ===== STEP 3: Resolve the effective client key (header, field, or both — see ADR-014) =====
	var headerKey string
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get(observability.MetadataKeyIdempotencyKey); len(vals) > 0 {
			headerKey = vals[0]
		}
	}
	clientKey, keyErr := resolveIdempotencyKey(headerKey, issueReq.IdempotencyKey)
	if keyErr != nil {
		return nil, keyErr
	}
	if clientKey == "" {
		return handler(ctx, req)
	}

	// ===== STEP 4: Construct Redis key =====
	key := idempotencyRedisPrefix + idempotencyKeySep + tenantID + idempotencyKeySep + idempotencyMethodIssue + idempotencyKeySep + clientKey

	// ===== STEP 4.5: Compute this request's content fingerprint =====
	fingerprint := computeIssueFingerprint(issueReq)

	missLabels := map[string]string{
		idempotencyLabelResult: idempotencyResultMiss,
		idempotencyLabelMethod: info.FullMethod,
	}
	hitLabels := map[string]string{
		idempotencyLabelResult: idempotencyResultHit,
		idempotencyLabelMethod: info.FullMethod,
	}
	mismatchLabels := map[string]string{
		idempotencyLabelResult: idempotencyResultMismatch,
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
			resp, storedFingerprint, isCompleted, resolveErr := resolveExistingRecord(cached)
			if resolveErr != nil {
				logger.Warn(ctx, "idempotency cached record unreadable; treating as concurrent duplicate", "error", resolveErr)
			} else if isCompleted {
				if storedFingerprint != "" && storedFingerprint != fingerprint {
					metrics.IncrementCounter(observability.MetricIdempotencyTotal, mismatchLabels)
					return nil, status.Error(codes.FailedPrecondition, idempotencyFingerprintMismatchMsg)
				}
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
			if recordBytes, encodeErr := encodeCompletedRecord(marshaledBytes, fingerprint); encodeErr == nil {
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
	// ===== STEP 2: Extract tenantID via type assertion =====
	refreshReq, ok := req.(*tokenv1.RefreshTokenRequest)
	if !ok {
		logger.Error(ctx, "idempotency interceptor: req type assertion to *RefreshTokenRequest failed")
		return handler(ctx, req)
	}
	tenantID := refreshReq.TenantId
	if tenantID == "" {
		tenantID = idempotencyDefaultTenantID
	}

	// ===== STEP 3: Resolve the effective client key (header, field, or both — see ADR-014) =====
	var headerKey string
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get(observability.MetadataKeyIdempotencyKey); len(vals) > 0 {
			headerKey = vals[0]
		}
	}
	clientKey, keyErr := resolveIdempotencyKey(headerKey, refreshReq.IdempotencyKey)
	if keyErr != nil {
		return nil, keyErr
	}
	if clientKey == "" {
		return handler(ctx, req)
	}

	// ===== STEP 4: Construct Redis key =====
	key := idempotencyRedisPrefix + idempotencyKeySep + tenantID + idempotencyKeySep + idempotencyMethodRefresh + idempotencyKeySep + clientKey

	// ===== STEP 4.5: Compute this request's content fingerprint =====
	fingerprint := computeRefreshFingerprint(refreshReq)

	missLabels := map[string]string{
		idempotencyLabelResult: idempotencyResultMiss,
		idempotencyLabelMethod: info.FullMethod,
	}
	hitLabels := map[string]string{
		idempotencyLabelResult: idempotencyResultHit,
		idempotencyLabelMethod: info.FullMethod,
	}
	mismatchLabels := map[string]string{
		idempotencyLabelResult: idempotencyResultMismatch,
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
			resp, storedFingerprint, isCompleted, resolveErr := resolveExistingRecord(cached)
			if resolveErr != nil {
				logger.Warn(ctx, "idempotency cached record unreadable; treating as concurrent duplicate", "error", resolveErr)
			} else if isCompleted {
				if storedFingerprint != "" && storedFingerprint != fingerprint {
					metrics.IncrementCounter(observability.MetricIdempotencyTotal, mismatchLabels)
					return nil, status.Error(codes.FailedPrecondition, idempotencyFingerprintMismatchMsg)
				}
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
			if recordBytes, encodeErr := encodeCompletedRecord(marshaledBytes, fingerprint); encodeErr == nil {
				if setErr := st.Set(ctx, key, recordBytes); setErr != nil {
					logger.Warn(ctx, "idempotency store Set (promote) error", "error", setErr)
				}
			}
		}
	}

	metrics.IncrementCounter(observability.MetricIdempotencyTotal, missLabels)
	return resp, nil
}
