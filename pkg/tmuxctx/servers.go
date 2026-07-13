package tmuxctx

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
)

type serverSpec struct {
	Identity string
	Args     []string
}

func discoverServers(ctx context.Context, env Env, lister ServerProcessLister) ([]serverSpec, error) {
	processes, err := lister(ctx)
	if err != nil {
		return nil, err
	}

	servers := make([]serverSpec, 0, len(processes)+1)
	seen := make(map[string]struct{}, len(processes)+1)
	add := func(server serverSpec) {
		key := server.Identity
		if key == "" {
			key = "default"
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		servers = append(servers, server)
	}

	if socket := tmuxServerSocket(env.TMUX); socket != "" {
		add(serverSpec{Identity: socket, Args: []string{"-S", socket}})
	} else {
		add(serverSpec{Identity: "default", Args: nil})
	}

	for _, process := range processes {
		server, ok := serverSpecFromArgs(process.Args)
		if !ok {
			continue
		}
		add(server)
	}
	return servers, nil
}

func serverSpecFromArgs(args []string) (serverSpec, bool) {
	if !isTmuxServerArgs(args) {
		return serverSpec{Identity: "", Args: nil}, false
	}
	for index, arg := range args {
		switch arg {
		case "-S":
			if index+1 >= len(args) || strings.TrimSpace(args[index+1]) == "" {
				return serverSpec{Identity: "", Args: nil}, false
			}
			socket := args[index+1]
			return serverSpec{Identity: socket, Args: []string{"-S", socket}}, true
		case "-L":
			if index+1 >= len(args) || strings.TrimSpace(args[index+1]) == "" {
				return serverSpec{Identity: "", Args: nil}, false
			}
			name := args[index+1]
			return serverSpec{Identity: "-L:" + name, Args: []string{"-L", name}}, true
		}
	}
	return serverSpec{Identity: "default", Args: nil}, true
}

func isTmuxServerArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for index, arg := range args {
		base := filepath.Base(arg)
		if strings.HasPrefix(base, "tmux: server") {
			return true
		}
		if index != 0 || (base != "tmux" && base != "tmux:") {
			continue
		}
		if slices.Contains(args[1:], "server") {
			return true
		}
	}
	return false
}
