// Package broker provides a lightweight, dependency-free client and wire protocol
// for communicating with the local AHT realtime broker over a Unix domain socket.
//
// Use [NewClientForSocket] or [NewClient] to connect directly to the broker daemon.
// Unlike [github.com/zigai/aht/pkg/client], operations against a broker Client
// never fall back to durable disk files or take filesystem locks; if the broker
// is stopped, operations fail immediately with [ErrUnavailable].
package broker
