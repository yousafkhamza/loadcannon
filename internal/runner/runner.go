package runner

import (
	"fmt"
	"os"
	"os/exec"
)

// Run executes k6 against the generated script, writing a JSON summary to
// summaryPath. authEnv is merged into the child process's environment only
// (never appended to args, never echoed) so secrets never appear in shell
// history, `ps aux`, or CI logs.
func Run(scriptPath, summaryPath string, authEnv map[string]string, extraArgs []string) error {
	if _, err := exec.LookPath("k6"); err != nil {
		return fmt.Errorf("k6 is not installed or not on PATH — install it first: https://k6.io/docs/get-started/installation/")
	}

	args := append([]string{"run",
		"--summary-export", summaryPath,
	}, extraArgs...)
	args = append(args, scriptPath)

	cmd := exec.Command("k6", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	env := os.Environ()
	for k, v := range authEnv {
		env = append(env, k+"="+v)
	}
	cmd.Env = env

	return cmd.Run()
}
