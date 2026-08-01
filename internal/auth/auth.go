// Package auth resolves credential references into actual secret values at
// run time, and only at run time. loadcannon scenario files never contain
// plaintext secrets — only a source (env|file|ssm|prompt) and a reference
// (env var name, file path, or SSM parameter name).
//
// Resolved values are:
//   - never written to disk
//   - never interpolated into the generated k6 script text
//   - never passed as CLI arguments to the k6 subprocess (visible in `ps aux`)
//   - passed to k6 only via the child process's environment block, read on
//     the JS side with __ENV
package auth

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/yousafkhamza/loadcannon/internal/config"
)

// Resolve returns the plaintext secret for the given source+ref.
func Resolve(source config.SecretSource, ref, promptLabel string) (string, error) {
	switch source {
	case config.SourceEnv:
		v, ok := os.LookupEnv(ref)
		if !ok || v == "" {
			return "", fmt.Errorf("environment variable %s is not set", ref)
		}
		return v, nil

	case config.SourceFile:
		b, err := os.ReadFile(ref)
		if err != nil {
			return "", fmt.Errorf("reading secret file %s: %w", ref, err)
		}
		return strings.TrimSpace(string(b)), nil

	case config.SourceSSM:
		// Shells out to the AWS CLI rather than vendoring the AWS SDK, so
		// loadcannon stays a single dependency-free binary and reuses
		// whatever AWS credentials/role are already configured on the host
		// (matches how manage-users.sh resolves SSM SecureString values).
		out, err := exec.Command("aws", "ssm", "get-parameter",
			"--name", ref,
			"--with-decryption",
			"--query", "Parameter.Value",
			"--output", "text",
		).Output()
		if err != nil {
			return "", fmt.Errorf("resolving SSM parameter %s (is `aws` configured and does the caller have ssm:GetParameter?): %w", ref, err)
		}
		return strings.TrimSpace(string(out)), nil

	case config.SourcePrompt:
		return promptMasked(promptLabel)

	default:
		return "", fmt.Errorf("unknown secret source %q", source)
	}
}

// promptMasked reads a line from the terminal without echoing it, using
// stty rather than a third-party terminal library. Falls back to visible
// input if stty isn't available (e.g. non-interactive shells) so the tool
// still works in that case, with a warning.
func promptMasked(label string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", label)

	if err := exec.Command("stty", "-F", "/dev/tty", "-echo").Run(); err != nil {
		fmt.Fprintln(os.Stderr, "\n[warn] could not disable terminal echo; input will be visible")
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	defer exec.Command("stty", "-F", "/dev/tty", "echo").Run()

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// BuildAuthEnv resolves whatever the scenario's auth block requires and
// returns a map of env var names -> resolved values to inject into the k6
// subprocess. The names are fixed (LOADCANNON_AUTH_*) and referenced by the
// generated k6 script; they are not derived from the ref itself, so refs
// like SSM parameter paths never leak into env var names or logs.
func BuildAuthEnv(c *config.Config) (map[string]string, error) {
	env := map[string]string{}
	switch c.Auth.AuthMode {
	case config.AuthNone:
		return env, nil

	case config.AuthBearer, config.AuthAPIKey:
		v, err := Resolve(c.Auth.TokenSource, c.Auth.TokenRef, "Enter token")
		if err != nil {
			return nil, err
		}
		env["LOADCANNON_AUTH_HEADER"] = c.Auth.Header
		env["LOADCANNON_AUTH_VALUE"] = c.Auth.Prefix + v
		return env, nil

	case config.AuthBasic:
		u, err := Resolve(c.Auth.UsernameSource, c.Auth.UsernameRef, "Enter username")
		if err != nil {
			return nil, err
		}
		p, err := Resolve(c.Auth.PasswordSource, c.Auth.PasswordRef, "Enter password")
		if err != nil {
			return nil, err
		}
		env["LOADCANNON_AUTH_USER"] = u
		env["LOADCANNON_AUTH_PASS"] = p
		return env, nil

	default:
		return nil, fmt.Errorf("unknown auth mode %q", c.Auth.AuthMode)
	}
}
