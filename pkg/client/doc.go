// Package client provides access to the current user's AHT agent-session state.
//
// A Client uses the local realtime broker when it is available and falls back
// to the durable registry for one-shot operations. Use [Client.Watch] when a
// program needs an initial snapshot followed by live state revisions.
package client
