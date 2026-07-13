//go:build !linux && !darwin

package processinfo

import (
	"context"
	"runtime"
)

// List reports that this operating system has no supported process observer.
func List(_ context.Context) ([]Process, error) {
	return nil, &UnsupportedError{Platform: runtime.GOOS}
}

// StartIdentity returns a stable process start identity for pid when the
// platform exposes one through the local processinfo implementation.
func StartIdentity(_ context.Context, _ int) string {
	return ""
}

// CommandName returns the executable command name for pid when the platform
// exposes one through the local processinfo implementation.
func CommandName(_ context.Context, _ int) (string, error) {
	return "", nil
}
