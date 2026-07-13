//go:build !linux && !darwin

package reportqueue

func syscallKillZero(int) error {
	return nil
}
