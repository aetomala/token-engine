package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/aetomala/jwtauth/pkg/keys"
	"github.com/aetomala/token-engine/internal/audit"
	"github.com/redis/go-redis/v9"
)

// Checker name constants used in readiness response JSON.
const (
	CheckerNameRedis           = "redis"
	CheckerNameKeyAvailability = "key_availability"
	CheckerNameAudit           = "audit"
)

// Checker is the extension interface for readiness checks.
type Checker interface {
	// Name returns a stable identifier for this check. Included in the JSON response body.
	Name() string

	// Check performs the readiness check. Must return within a reasonable timeout.
	// Returns nil when healthy, a descriptive error when unhealthy.
	// The error message is included in the JSON body — must not contain sensitive details.
	Check(ctx context.Context) error
}

// NewLiveHandler returns an HTTP handler that always responds with 200 OK.
func NewLiveHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// NewReadyHandler returns an HTTP handler that runs the registered checkers in order.
// All checkers must pass for a 200 response. The first failure returns 503 with a JSON body.
// An empty or nil checkers slice is treated as vacuously healthy (returns 200).
func NewReadyHandler(checkers []Checker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, checker := range checkers {
			if err := checker.Check(r.Context()); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				resp := readyResponse{
					Status:      "unavailable",
					FailedCheck: checker.Name(),
					Reason:      err.Error(),
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	})
}

type readyResponse struct {
	Status      string `json:"status"`
	FailedCheck string `json:"failed_check"`
	Reason      string `json:"reason"`
}

// RedisChecker checks Redis reachability via PING.
type RedisChecker struct {
	client *redis.Client
}

var _ Checker = (*RedisChecker)(nil)

// NewRedisChecker returns a new RedisChecker using the provided client.
func NewRedisChecker(client *redis.Client) *RedisChecker {
	return &RedisChecker{client: client}
}

// Name returns the stable identifier for this checker.
func (c *RedisChecker) Name() string { return CheckerNameRedis }

// Check pings Redis and returns an error if unreachable.
func (c *RedisChecker) Check(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// KeyAvailabilityChecker checks that at least one signing key is cached in the KeyManager.
type KeyAvailabilityChecker struct {
	km keys.KeyManager
}

var _ Checker = (*KeyAvailabilityChecker)(nil)

// NewKeyAvailabilityChecker returns a new KeyAvailabilityChecker using the provided KeyManager.
func NewKeyAvailabilityChecker(km keys.KeyManager) *KeyAvailabilityChecker {
	return &KeyAvailabilityChecker{km: km}
}

// Name returns the stable identifier for this checker.
func (c *KeyAvailabilityChecker) Name() string { return CheckerNameKeyAvailability }

// Check returns an error if no signing keys are available in the KeyManager.
func (c *KeyAvailabilityChecker) Check(ctx context.Context) error {
	// ===== STEP 1: Get all key info =====
	infos, err := c.km.GetAllKeyInfo(ctx)
	if err != nil {
		return err
	}
	// ===== STEP 2: Require at least one key =====
	if len(infos) == 0 {
		return errors.New("no signing keys available")
	}
	return nil
}

// AuditChecker is a health.Checker that verifies the audit store is reachable
// by calling audit.Store.Ping. All methods are safe for concurrent use.
type AuditChecker struct {
	store audit.Store
}

// Compile-time assertion — place immediately after struct declaration.
var _ Checker = (*AuditChecker)(nil)

// NewAuditChecker constructs an AuditChecker. Store must not be nil.
func NewAuditChecker(store audit.Store) *AuditChecker {
	return &AuditChecker{store: store}
}

// Name returns the stable identifier for this checker.
func (c *AuditChecker) Name() string { return CheckerNameAudit }

// Check calls audit.Store.Ping and returns any error.
// Returns the context error if the context is cancelled.
func (c *AuditChecker) Check(ctx context.Context) error {
	return c.store.Ping(ctx)
}
