package recorder

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// restoreExecCommand returns a cleanup function that restores the original execCommand.
func swapExecCommand(fake func(string, ...string) *exec.Cmd) func() {
	orig := execCommand
	execCommand = fake
	return func() { execCommand = orig }
}

func TestRecord_Success(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "out.ansi")

	restore := swapExecCommand(func(name string, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "exit 0")
	})
	defer restore()

	if err := Record(outputPath, "echo", []string{"hello"}); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestRecord_CommandFailure(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "out.ansi")

	restore := swapExecCommand(func(name string, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "exit 42")
	})
	defer restore()

	err := Record(outputPath, "false", nil)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "42") {
		t.Errorf("expected error message to contain exit code 42, got: %v", err)
	}
}

func TestRecord_ArgsPassedCorrectly(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "out.ansi")

	var gotName string
	var gotArgs []string

	restore := swapExecCommand(func(name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = args
		return exec.Command("sh", "-c", "exit 0")
	})
	defer restore()

	if err := Record(outputPath, "mycommand", []string{"arg1", "arg2"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotName != "script" {
		t.Errorf("expected command %q, got %q", "script", gotName)
	}
	wantArgs := []string{"-q", outputPath, "mycommand", "arg1", "arg2"}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("expected args %v, got %v", wantArgs, gotArgs)
	}
	for i, a := range wantArgs {
		if gotArgs[i] != a {
			t.Errorf("args[%d]: expected %q, got %q", i, a, gotArgs[i])
		}
	}
}
