//go:build !windows && !linux && !darwin

package harnessboard

import "fmt"

func listProcessesOS() ([]Process, error) {
	return nil, fmt.Errorf("process listing is not supported on this OS")
}
