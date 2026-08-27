//go:build !unix

package cli

func terminalWidth(_ any) int {
	return 0
}
