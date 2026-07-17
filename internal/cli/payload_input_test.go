package cli

import (
	"strings"
	"testing"
)

func TestReadPayloadInputAcceptsExactLimit(t *testing.T) {
	t.Parallel()

	data, err := readPayloadInput(strings.NewReader(strings.Repeat("x", maxPayloadInputBytes)))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != maxPayloadInputBytes {
		t.Fatalf("payload length = %d, want %d", len(data), maxPayloadInputBytes)
	}
}
