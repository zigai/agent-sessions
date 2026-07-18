//go:build !linux && !darwin

package service

func platformBackend(Options) (backend, error) { return nil, ErrUnsupported }
