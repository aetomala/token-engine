// Package config provides service-global configuration loaded from environment variables.
// It owns startup validation and fatal-exit behavior for required fields.
// It does not own observability wiring, network connections, or component construction.
// Primary dependency: os.Getenv.
package config
