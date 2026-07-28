// Package config provides service-global configuration loaded from environment variables.
// It owns startup validation, returning descriptive sentinel errors for required-field
// violations — callers decide whether to exit.
// It does not own observability wiring, network connections, or component construction.
// Primary dependency: os.Getenv.
package config
