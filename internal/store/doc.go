// Package store provides the idempotency store interface and its NoOp implementation.
// Redis implementation is deferred to v0.4. It does not own connection management.
// Primary dependency: context for request-scoped operations.
package store
