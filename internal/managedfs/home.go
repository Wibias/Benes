package managedfs

import (
	"fmt"
	"path/filepath"
)

var ErrCrossHome = fmt.Errorf("managed launch stages disagree on the client home")

func AssertSameHome(homes ...string) error {
	if len(homes) == 0 {
		return fmt.Errorf("managed client home is required")
	}
	var canon string
	for i, home := range homes {
		if home == "" {
			return fmt.Errorf("managed client home is required")
		}
		abs, err := filepath.Abs(home)
		if err != nil {
			return err
		}
		abs = filepath.Clean(abs)
		if i == 0 {
			canon = abs
			continue
		}
		if !sameFilepath(canon, abs) {
			return ErrCrossHome
		}
	}
	return nil
}

func sameFilepath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}
