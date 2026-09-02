package opencode

import _ "embed"

//go:embed assets/screen.toml
var screenManifest string

func (openCodeHarness) ScreenManifest() string {
	return screenManifest
}
