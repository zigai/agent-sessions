// Package brokerapi provides backward-compatible aliases for [github.com/zigai/aht/pkg/broker].
//
// Deprecated: Use [github.com/zigai/aht/pkg/broker] instead.
package brokerapi

import (
	"github.com/zigai/aht/pkg/broker"
)

type (
	Client       = broker.Client
	Subscription = broker.Subscription
	Store        = broker.Store
	Request      = broker.Request
	Response     = broker.Response
)

const (
	ProtocolVersion    = broker.ProtocolVersion
	MethodPing         = broker.MethodPing
	MethodObserve      = broker.MethodObserve
	MethodObserveBatch = broker.MethodObserveBatch
	MethodList         = broker.MethodList
	MethodGet          = broker.MethodGet
	MethodSummary      = broker.MethodSummary
	MethodGC           = broker.MethodGC
	MethodSubscribe    = broker.MethodSubscribe
	SocketPathEnv      = broker.SocketPathEnv
)

var (
	ErrUnavailable = broker.ErrUnavailable
	ErrProtocol    = broker.ErrProtocol

	NewClient          = broker.NewClient
	NewClientForSocket = broker.NewClientForSocket
	NewStore           = broker.NewStore
	NewStoreForSocket  = broker.NewStoreForSocket
	SocketPath         = broker.SocketPath
	DefaultSocketPath  = broker.DefaultSocketPath
	IsUnavailable      = broker.IsUnavailable
)
