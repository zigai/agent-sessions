package config

import (
	"fmt"
	"io"
	"os"

	"github.com/knadh/koanf/parsers/toml/v2"
)

// boundedConfigFile implements Koanf's provider contract without trusting an
// earlier Stat: the file may have grown or been replaced before it is opened.
type boundedConfigFile string

func (path boundedConfigFile) ReadBytes() ([]byte, error) {
	file, err := os.Open(string(path))
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	// Read-only close cannot lose configuration data.
	defer func() { _ = file.Close() }()
	contents, err := io.ReadAll(io.LimitReader(file, maxConfigFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if len(contents) > maxConfigFileSize {
		return nil, fmt.Errorf("%w: %s", ErrConfigFileTooLarge, path)
	}
	return contents, nil
}

func (path boundedConfigFile) Read() (map[string]any, error) {
	contents, err := path.ReadBytes()
	if err != nil {
		return nil, err
	}
	values, err := toml.Parser().Unmarshal(contents)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return values, nil
}
