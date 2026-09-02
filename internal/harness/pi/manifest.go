package pi

import _ "embed"

//go:embed assets/screen.toml
var screenManifest string

func (piHarness) ScreenManifest() string {
	return screenManifest
}
