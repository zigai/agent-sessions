// Package registry models and stores coding-agent harness state.
//
// Observations from native harness events, process discovery, terminal
// locations, catalogs, and screen state are reduced into a stable [Session]
// view. [FileStore] provides atomic durable storage, while [MemoryStore]
// provides a realtime in-memory authority with explicit persistence.
//
// Programs that only need to inspect the current user's running AHT instance
// should generally use package client instead of opening the registry directly.
package registry
