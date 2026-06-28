//go:build !linux && !darwin

package processinfo

import "context"

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
