//go:build !linux && !darwin

package service

import (
	"context"
	"runtime"
)

type unsupportedBackend struct{}

func platformBackend(Options) (backend, error) { return nil, ErrUnsupported }

func (unsupportedBackend) describe() Result {
	return Result{Platform: runtime.GOOS, Manager: "unsupported", ManagedVersion: managedVersion, Version: managedVersion}
}
func (unsupportedBackend) reload(context.Context, CommandExecutor) error { return ErrUnsupported }

func (unsupportedBackend) content() string                             { return "" }
func (unsupportedBackend) load(context.Context, CommandExecutor) error { return ErrUnsupported }

func (unsupportedBackend) restart(context.Context, CommandExecutor) error { return ErrUnsupported }
func (unsupportedBackend) unload(context.Context, CommandExecutor) error  { return ErrUnsupported }
func (unsupportedBackend) running(context.Context, CommandExecutor) (bool, string) {
	return false, "unsupported"
}
