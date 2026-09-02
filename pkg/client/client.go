package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zigai/aht/internal/brokerapi"
	"github.com/zigai/aht/pkg/registry"
)

var (
	// ErrUnavailable means no realtime AHT broker accepted the local connection.
	ErrUnavailable = errors.New("aht broker unavailable")
	// ErrProtocol means the broker returned an invalid or incompatible response.
	ErrProtocol        = errors.New("aht broker protocol error")
	errHandlerRequired = errors.New("watch handler is required")
)

// Config identifies the local AHT instance used by a Client. Empty fields use
// the current user's default registry and its associated broker socket.
type Config struct {
	StorePath  string
	SocketPath string
}

// Client reads and updates agent-harness state through the local AHT broker.
// One-shot operations fall back to the durable registry when the broker is not
// running. Watch requires a running broker.
type Client struct {
	storePath string
	store     *brokerapi.Store
	realtime  *brokerapi.Client
}

var _ registry.Store = (*Client)(nil)

// New returns a client for the configured local AHT instance.
func New(config Config) *Client {
	storePath := config.StorePath
	if storePath == "" {
		storePath = registry.DefaultStorePath()
	}

	socketPath := config.SocketPath
	if socketPath == "" {
		socketPath = brokerapi.SocketPath(storePath)
	}

	store := brokerapi.NewStoreForSocket(storePath, socketPath)
	return &Client{storePath: storePath, store: store, realtime: store.Client()}
}

// StorePath returns the durable registry path used for broker fallback.
func (c *Client) StorePath() string {
	return c.storePath
}

// SocketPath returns the broker endpoint used by the client.
func (c *Client) SocketPath() string {
	return c.realtime.SocketPath()
}

// Ping verifies that the realtime broker is accepting requests.
func (c *Client) Ping(ctx context.Context) error {
	return publicError(c.realtime.Ping(ctx))
}

// Observe records one agent-harness observation. When the broker is not
// running, the client records the observation directly in the durable registry.
func (c *Client) Observe(ctx context.Context, observation registry.Observation) (registry.Session, error) {
	session, err := c.store.Observe(ctx, observation)
	return session, publicError(err)
}

// ObserveBatch atomically records a group of agent-harness observations.
func (c *Client) ObserveBatch(ctx context.Context, observations []registry.Observation) ([]registry.Session, error) {
	sessions, err := c.store.ObserveBatch(ctx, observations)
	return sessions, publicError(err)
}

// List returns all sessions matching filter.
func (c *Client) List(ctx context.Context, filter registry.Filter) ([]registry.Session, error) {
	sessions, err := c.store.List(ctx, filter)
	return sessions, publicError(err)
}

// Get returns the session identified by id.
func (c *Client) Get(ctx context.Context, id string) (registry.Session, error) {
	session, err := c.store.Get(ctx, id)
	return session, publicError(err)
}

// Summary returns aggregate session counts grouped by terminal-multiplexer session.
func (c *Client) Summary(ctx context.Context, filter registry.Filter) ([]registry.Summary, error) {
	summaries, err := c.store.SummaryByTmuxSession(ctx, filter)
	return summaries, publicError(err)
}

// SummaryByTmuxSession implements registry.Store.
func (c *Client) SummaryByTmuxSession(ctx context.Context, filter registry.Filter) ([]registry.Summary, error) {
	return c.Summary(ctx, filter)
}

// SummaryByTmuxSessionWithOptions implements registry.Store.
func (c *Client) SummaryByTmuxSessionWithOptions(ctx context.Context, options registry.SummaryOptions) ([]registry.Summary, error) {
	return c.Summary(ctx, options.Filter)
}

// GC removes gone-session tombstones at least deleteAfter old.
func (c *Client) GC(ctx context.Context, deleteAfter time.Duration) (registry.GCResult, error) {
	result, err := c.store.GC(ctx, deleteAfter)
	return result, publicError(err)
}

// Watch calls yield with the initial filtered snapshot and each strictly newer
// revision until ctx is canceled. Watch returns nil after cancellation.
func (c *Client) Watch(
	ctx context.Context,
	filter registry.Filter,
	yield func(registry.StateSnapshot) error,
) error {
	if yield == nil {
		return errHandlerRequired
	}

	subscription, err := c.realtime.Subscribe(ctx, filter)
	if err != nil {
		return publicError(err)
	}
	defer subscription.Close()

	snapshots := subscription.Snapshots
	errorsChannel := subscription.Errors
	for snapshots != nil || errorsChannel != nil {
		select {
		case <-ctx.Done():
			return nil
		case snapshot, ok := <-snapshots:
			if !ok {
				snapshots = nil
				continue
			}
			if err := yield(snapshot); err != nil {
				return fmt.Errorf("handling AHT state snapshot: %w", err)
			}
		case watchErr, ok := <-errorsChannel:
			if !ok {
				errorsChannel = nil
				continue
			}
			if watchErr != nil {
				return publicError(watchErr)
			}
		}
	}

	return nil
}

// IsUnavailable reports whether err means that no realtime broker accepted the connection.
func IsUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}

// OperationError is a machine-readable failure returned by the AHT broker.
type OperationError struct {
	Code    string
	Message string
}

func (e *OperationError) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

func publicError(err error) error {
	if err == nil {
		return nil
	}
	if brokerapi.IsUnavailable(err) {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if errors.Is(err, brokerapi.ErrProtocol) {
		return fmt.Errorf("%w: %w", ErrProtocol, err)
	}

	if remoteError, ok := errors.AsType[*brokerapi.RemoteError](err); ok {
		return &OperationError{Code: remoteError.Code, Message: remoteError.Message}
	}
	return err
}
