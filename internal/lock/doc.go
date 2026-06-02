// Package lock provides a distributed lock interface and Redis-backed implementation.
// RedisLock uses SET NX PX for acquisition and a Lua compare-and-delete script for release.
package lock
