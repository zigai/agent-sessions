//go:build !linux && !darwin

package broker

import (
	"context"
	"net"
)

func listenLocal(context.Context, string) (net.Listener, error) { return nil, ErrUnsupported }

func cleanupLocal(string, net.Listener) {}
