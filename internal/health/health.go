package health

import (
	"context"
	"encoding/json"
	"net/http"
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
