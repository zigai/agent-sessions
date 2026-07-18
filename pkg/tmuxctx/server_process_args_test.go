package tmuxctx

import (
	"encoding/binary"
	"errors"
	"slices"
	"testing"
)

func TestParseDarwinProcArgsPreservesArgumentBoundaries(t *testing.T) {
	t.Parallel()
	want := []string{"tmux", "-S", "/tmp/agent sessions.sock", "server"}
	data := make([]byte, 0, 96)
	data = binary.LittleEndian.AppendUint32(data, 4)
	data = append(data, "/usr/local/bin/tmux\x00\x00"...)
	for _, arg := range want {
		data = append(data, arg...)
		data = append(data, 0)
	}
	data = append(data, "PATH=/usr/bin\x00"...)

	got, err := parseDarwinProcArgs(data)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("parsed arguments = %#v, want %#v", got, want)
	}
	server, ok := serverSpecFromArgs(got)
	if !ok || server.Identity != "/tmp/agent sessions.sock" || !slices.Equal(server.Args, []string{"-S", "/tmp/agent sessions.sock"}) {
		t.Fatalf("server spec lost socket argument boundary: %#v, %t", server, ok)
	}
}

func TestParseDarwinProcArgsRejectsTruncation(t *testing.T) {
	t.Parallel()
	data := make([]byte, 0, 24)
	data = binary.LittleEndian.AppendUint32(data, 2)
	data = append(data, "/usr/bin/tmux\x00\x00tmux\x00"...)
	if _, err := parseDarwinProcArgs(data); !errors.Is(err, errInvalidDarwinProcArgs) {
		t.Fatalf("truncated arguments error = %v", err)
	}
}
