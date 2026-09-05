// Package client provides access to the current user's AHT agent-session state.
//
// By default, a Client uses the local realtime broker when it is available and
// falls back to the durable registry for one-shot operations. [Config.Mode] can
// instead require realtime or durable storage; invalid modes make operations
// fail with [ErrInvalidMode]. Use [Client.Watch] when a
// program needs an initial snapshot followed by live state revisions.
package client
