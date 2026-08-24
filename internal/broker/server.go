package broker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/zigai/agent-sessions/v2/pkg/brokerapi"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const (
	maxRequestBytes       = 16 << 20
	maxBatchObservations  = 4096
	defaultMaxConnections = 256
	scannerInitialBytes   = 4 << 10
	handshakeTimeout      = 5 * time.Second
	writeTimeout          = 2 * time.Second
	heartbeatInterval     = 15 * time.Second
)

var (
	ErrAlreadyRunning = errors.New("agent-sessions broker is already running")
	ErrUnsupported    = errors.New("agent-sessions broker is unsupported on this platform")

	errContextNil          = errors.New("broker context is nil")
	errStoreNil            = errors.New("broker store is nil")
	errSocketPathEmpty     = errors.New("broker socket path is empty")
	errProtocolVersion     = errors.New("unsupported broker protocol version")
	errRequestIDRequired   = errors.New("broker request id is required")
	errUnknownMethod       = errors.New("unknown broker method")
	errObservationRequired = errors.New("observe requires observation")
	errBatchRequired       = errors.New("observe_batch requires observations")
	errBatchTooLarge       = errors.New("observe_batch has too many observations")
	errSessionIDRequired   = errors.New("get requires session_id")
	errPathNotSocket       = errors.New("broker path is not a socket")
)

// Server owns the local socket API for one in-memory registry.
type Server struct {
	store          *registry.MemoryStore
	socketPath     string
	maxConnections int
	ready          chan<- struct{}
}

// Options configures a broker server.
type Options struct {
	Store          *registry.MemoryStore
	SocketPath     string
	MaxConnections int
	Ready          chan<- struct{}
}

// New returns a broker server. Serve validates required dependencies.
func New(options Options) *Server {
	maxConnections := options.MaxConnections
	if maxConnections <= 0 {
		maxConnections = defaultMaxConnections
	}

	return &Server{
		store:          options.Store,
		socketPath:     options.SocketPath,
		maxConnections: maxConnections,
		ready:          options.Ready,
	}
}

// Serve accepts local requests until ctx is canceled.
func (s *Server) Serve(ctx context.Context) error {
	if ctx == nil {
		return errContextNil
	}
	if s.store == nil {
		return errStoreNil
	}
	if s.socketPath == "" {
		return errSocketPathEmpty
	}

	listener, err := listenLocal(ctx, s.socketPath)
	if err != nil {
		return err
	}
	defer cleanupLocal(s.socketPath, listener)
	stopClose := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stopClose()

	if s.ready != nil {
		close(s.ready)
	}

	semaphore := make(chan struct{}, s.maxConnections)
	var connections sync.WaitGroup
	for {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil || errors.Is(acceptErr, net.ErrClosed) {
				break
			}

			return fmt.Errorf("accepting broker connection: %w", acceptErr)
		}

		select {
		case semaphore <- struct{}{}:
			connections.Go(func() {
				defer func() { <-semaphore }()
				s.handleConnection(ctx, connection)
			})
		default:
			_ = connection.Close()
		}
	}

	connections.Wait()

	return nil
}

func (s *Server) handleConnection(ctx context.Context, connection net.Conn) {
	defer func() { _ = connection.Close() }()
	_ = connection.SetReadDeadline(time.Now().Add(handshakeTimeout))

	scanner := bufio.NewScanner(connection)
	scanner.Buffer(make([]byte, scannerInitialBytes), maxRequestBytes)
	if !scanner.Scan() {
		return
	}

	var request brokerapi.Request
	//nolint:musttag // Request defines the complete public JSON protocol schema.
	if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
		_ = writeResponse(connection, errorResponse(request.ID, "invalid_json", err))
		return
	}
	if err := validateRequest(request); err != nil {
		_ = writeResponse(connection, errorResponse(request.ID, "invalid_request", err))
		return
	}
	_ = connection.SetReadDeadline(time.Time{})

	if request.Method == brokerapi.MethodSubscribe {
		s.serveSubscription(ctx, connection, request)
		return
	}

	response := s.execute(ctx, request)
	_ = writeResponse(connection, response)
}

func validateRequest(request brokerapi.Request) error {
	if request.Version != brokerapi.ProtocolVersion {
		return fmt.Errorf("%w: %d", errProtocolVersion, request.Version)
	}
	if request.ID == "" {
		return errRequestIDRequired
	}

	switch request.Method {
	case brokerapi.MethodPing,
		brokerapi.MethodObserve,
		brokerapi.MethodObserveBatch,
		brokerapi.MethodList,
		brokerapi.MethodGet,
		brokerapi.MethodSummary,
		brokerapi.MethodGC,
		brokerapi.MethodSubscribe:
	default:
		return fmt.Errorf("%w: %q", errUnknownMethod, request.Method)
	}

	if request.Method == brokerapi.MethodObserve && request.Observation == nil {
		return errObservationRequired
	}
	if request.Method == brokerapi.MethodObserveBatch {
		if len(request.Observations) == 0 {
			return errBatchRequired
		}
		if len(request.Observations) > maxBatchObservations {
			return fmt.Errorf("%w: maximum %d", errBatchTooLarge, maxBatchObservations)
		}
	}
	if request.Method == brokerapi.MethodGet && request.SessionID == "" {
		return errSessionIDRequired
	}

	return nil
}

func (s *Server) execute(ctx context.Context, request brokerapi.Request) brokerapi.Response {
	response := newResponse(request.ID, "result")

	var err error
	switch request.Method {
	case brokerapi.MethodPing:
		response.Now = time.Now().UTC()
	case brokerapi.MethodObserve:
		var session registry.Session
		session, err = s.store.Observe(ctx, *request.Observation)
		response.Session = &session
	case brokerapi.MethodObserveBatch:
		response.Sessions, err = s.store.ObserveBatch(ctx, request.Observations)
	case brokerapi.MethodList:
		response.Sessions, err = s.store.List(ctx, request.Filter)
	case brokerapi.MethodGet:
		var session registry.Session
		session, err = s.store.Get(ctx, request.SessionID)
		response.Session = &session
	case brokerapi.MethodSummary:
		response.Summaries, err = s.store.SummaryByTmuxSession(ctx, request.Filter)
	case brokerapi.MethodGC:
		var result registry.GCResult
		result, err = s.store.GC(ctx, request.DeleteAfter)
		response.GC = &result
	}
	if err != nil {
		return operationErrorResponse(request.ID, err)
	}

	return response
}

func (s *Server) serveSubscription(
	ctx context.Context,
	connection net.Conn,
	request brokerapi.Request,
) {
	state, err := s.store.State(ctx, request.Filter)
	if err != nil {
		_ = writeResponse(connection, operationErrorResponse(request.ID, err))
		return
	}
	if err := writeResponse(connection, snapshotResponse(request.ID, state)); err != nil {
		return
	}

	revision := state.Revision
	for {
		waitContext, cancel := context.WithTimeout(ctx, heartbeatInterval)
		next, waitErr := s.store.WaitForRevision(waitContext, revision, request.Filter)
		cancel()
		if waitErr != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(waitErr, context.DeadlineExceeded) {
				response := newResponse(request.ID, "heartbeat")
				response.Now = time.Now().UTC()
				if err := writeResponse(connection, response); err != nil {
					return
				}
				continue
			}

			_ = writeResponse(connection, operationErrorResponse(request.ID, waitErr))
			return
		}

		if err := writeResponse(connection, snapshotResponse(request.ID, next)); err != nil {
			return
		}
		revision = next.Revision
	}
}

func snapshotResponse(id string, state registry.StateSnapshot) brokerapi.Response {
	response := newResponse(id, "snapshot")
	response.Snapshot = &state

	return response
}

func errorResponse(id, code string, err error) brokerapi.Response {
	response := newResponse(id, "error")
	response.Error = &brokerapi.Error{Code: code, Message: err.Error()}

	return response
}

func newResponse(id, responseType string) brokerapi.Response {
	return brokerapi.Response{
		Version:   brokerapi.ProtocolVersion,
		ID:        id,
		Type:      responseType,
		Error:     nil,
		Session:   nil,
		Sessions:  nil,
		Summaries: nil,
		Snapshot:  nil,
		GC:        nil,
		Now:       time.Time{},
	}
}

func operationErrorResponse(id string, err error) brokerapi.Response {
	code := "operation_failed"
	switch {
	case errors.Is(err, registry.ErrSessionNotFound):
		code = "not_found"
	case errors.Is(err, registry.ErrObservationConflict):
		code = "observation_conflict"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code = "canceled"
	}

	return errorResponse(id, code, err)
}

func writeResponse(connection net.Conn, response brokerapi.Response) error {
	if err := connection.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return fmt.Errorf("setting broker write deadline: %w", err)
	}
	if err := json.NewEncoder(connection).Encode(response); err != nil {
		return fmt.Errorf("writing broker response: %w", err)
	}
	_ = connection.SetWriteDeadline(time.Time{})

	return nil
}
