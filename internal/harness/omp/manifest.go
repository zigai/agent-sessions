package omp

import _ "embed"

//go:embed assets/screen.toml
var screenManifest string

func (ompHarness) ScreenManifest() string {
	return screenManifest
}
