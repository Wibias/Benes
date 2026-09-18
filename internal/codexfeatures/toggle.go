package codexfeatures

import (
	"fmt"
	"os/exec"
	"strings"
)

var Run = func(action string) error {
	cmd := exec.Command("codex", "features", action, "multi_agent_v2")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func Toggle(enable bool) error {
	action := "disable"
	if enable {
		action = "enable"
	}
	return Run(action)
}
