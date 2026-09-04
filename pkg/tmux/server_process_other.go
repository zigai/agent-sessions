//go:build !linux && !darwin

package tmux

import "context"

func listCurrentUserTmuxServers(ctx context.Context) ([]ServerProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}
