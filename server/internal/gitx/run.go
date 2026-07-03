package gitx

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// GitError carries the exit status and stderr of a failed git invocation.
type GitError struct {
	Args     []string
	ExitCode int
	Stderr   string
}

func (e *GitError) Error() string {
	return fmt.Sprintf("git %s (exit %d): %s", strings.Join(e.Args, " "), e.ExitCode, strings.TrimSpace(e.Stderr))
}

// run executes git in dir with extra environment variables, returning stdout.
func run(dir string, extraEnv []string, args ...string) (string, error) {
	out, _, err := runFull(dir, extraEnv, nil, args...)
	return out, err
}

func runFull(dir string, extraEnv []string, stdin []byte, args ...string) (stdout, stderr string, err error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	cmd.Env = append(cmd.Env, extraEnv...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	if err != nil {
		code := -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		detail := errb.String()
		if strings.TrimSpace(detail) == "" {
			detail = out.String() // e.g. `commit` explains failures on stdout
		}
		return out.String(), errb.String(), &GitError{Args: args, ExitCode: code, Stderr: detail}
	}
	return out.String(), errb.String(), nil
}

var versionRe = regexp.MustCompile(`git version (\d+)\.(\d+)`)

// CheckGitVersion verifies the git binary exists and is at least 2.38
// (required for `git merge-tree --write-tree`).
func CheckGitVersion() error {
	out, err := run("", nil, "version")
	if err != nil {
		return fmt.Errorf("git binary not usable: %w", err)
	}
	m := versionRe.FindStringSubmatch(out)
	if m == nil {
		return fmt.Errorf("cannot parse git version from %q", strings.TrimSpace(out))
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major < 2 || (major == 2 && minor < 38) {
		return fmt.Errorf("git >= 2.38 required (found %s.%s)", m[1], m[2])
	}
	return nil
}
