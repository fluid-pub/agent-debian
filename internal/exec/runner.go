package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const maxOutputBytes = 64 * 1024

type Result struct {
	ExitCode   int
	StdoutTail string
	StderrTail string
	DurationMs int64
}

type Runner interface {
	Run(ctx context.Context, command string, args []string, env []string, workingDir string) (Result, error)
	// RunWithCred runs the child with optional POSIX credentials (Linux; cred == nil is identical to Run).
	RunWithCred(ctx context.Context, command string, args []string, env []string, workingDir string, cred *syscall.Credential) (Result, error)
}

type OSRunner struct{}

func NewOSRunner() *OSRunner {
	return &OSRunner{}
}

func (r *OSRunner) Run(ctx context.Context, command string, args []string, env []string, workingDir string) (Result, error) {
	return r.RunWithCred(ctx, command, args, env, workingDir, nil)
}

func (r *OSRunner) RunWithCred(ctx context.Context, command string, args []string, env []string, workingDir string, cred *syscall.Credential) (Result, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = env
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	attr := &syscall.SysProcAttr{Setpgid: true}
	if cred != nil {
		attr.Credential = cred
	}
	cmd.SysProcAttr = attr

	runErr := cmd.Run()
	result := Result{
		ExitCode:   exitCode(runErr),
		StdoutTail: tailString(stdout.String(), maxOutputBytes),
		StderrTail: tailString(stderr.String(), maxOutputBytes),
		DurationMs: time.Since(start).Milliseconds(),
	}

	if runErr != nil {
		return result, fmt.Errorf("command failed: %s %s: %w", command, strings.Join(args, " "), runErr)
	}
	return result, nil
}

func exitCode(runErr error) int {
	if runErr == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func tailString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}
