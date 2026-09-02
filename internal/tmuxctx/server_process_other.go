//go:build !linux && !darwin

package tmuxctx

import "context"

func listCurrentUserTmuxServers(ctx context.Context) ([]ServerProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}
