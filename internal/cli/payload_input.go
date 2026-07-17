package cli

import (
	"errors"
	"fmt"
	"io"
)

const maxPayloadInputBytes = 1 << 20

var errPayloadInputTooLarge = errors.New("payload exceeds 1 MiB limit")

func readPayloadInput(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxPayloadInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading payload input: %w", err)
	}
	if len(data) > maxPayloadInputBytes {
		return nil, errPayloadInputTooLarge
	}
	return data, nil
}
