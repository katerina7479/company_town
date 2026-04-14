package commands_test

import (
	"os/exec"
	"testing"
)

// TestCompletionScripts_parseCleanlyInZsh runs `zsh -n` on each contrib
// completion script to catch syntax errors before they ship. The test skips
// cleanly on CI runners that don't have zsh installed.
func TestCompletionScripts_parseCleanlyInZsh(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not installed; skipping completion parse check")
	}

	scripts := []string{
		"../../contrib/gt.zsh",
		"../../contrib/ct.zsh",
	}

	for _, f := range scripts {
		f := f
		t.Run(f, func(t *testing.T) {
			cmd := exec.Command("zsh", "-n", f)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s failed zsh parse: %v\n%s", f, err, out)
			}
		})
	}
}
