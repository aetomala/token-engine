// Package audit provides the audit log interface and NoOp implementation for compliance-grade
// event recording. It does not own storage backends — the Redis implementation is wired in v0.3.
// Primary dependency: context for request-scoped operations.
package audit
