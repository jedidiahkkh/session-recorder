package recorder

import (
	"fmt"
	"os"
	"os/exec"
)

// Record runs command with args under `script`, capturing all terminal output
// to outputPath. It blocks until the command exits. stdin/stdout/stderr are
// inherited from the calling process so the session is fully interactive.
func Record(outputPath string, command string, args []string) error {
	scriptArgs := append([]string{"-q", outputPath, command}, args...)
	cmd := exec.Command("script", scriptArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("command exited with code %d", exitErr.ExitCode())
		}
		return err
	}
	return nil
}
