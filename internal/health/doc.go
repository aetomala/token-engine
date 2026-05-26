// Package health provides HTTP health check handlers and the Checker extension interface.
// LiveHandler always returns 200 OK. ReadyHandler runs the registered Checker slice in order.
// It does not own the HTTP mux or server — those belong to main.go.
// Primary dependency: net/http for handler implementation.
package health
