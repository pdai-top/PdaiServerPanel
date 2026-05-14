//go:build windows

package execx

import (
	"context"
	"os"
	"os/exec"
	"time"

	"golang.org/x/sys/windows"
)

// DefaultWaitDelay matches the Unix implementation for consistency.
const DefaultWaitDelay = 5 * time.Second

// CommandContext is a Windows-safe fallback for local development builds.
// Windows does not support the Unix process-group kill semantics used by the
// production target platforms, so this version falls back to the standard
// library behavior.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 0
	return cmd
}

// ShellContext mirrors the Unix helper for local development builds.
func ShellContext(ctx context.Context, script string) *exec.Cmd {
	if _, err := exec.LookPath("bash"); err == nil {
		return CommandContext(ctx, "bash", "-c", script)
	}
	if _, err := exec.LookPath("sh"); err == nil {
		return CommandContext(ctx, "sh", "-c", script)
	}
	return CommandContext(ctx, "cmd", "/C", script)
}

// BashContext is kept for existing callers.
func BashContext(ctx context.Context, script string) *exec.Cmd {
	return ShellContext(ctx, script)
}

// KillProcessGroup falls back to killing only the root process on Windows.
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// KillProcessGroupPID falls back to killing only the process on Windows.
func KillProcessGroupPID(pid int) error {
	if pid <= 0 {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

// ProcessAlive reports whether a PID still appears to exist.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	windows.CloseHandle(handle)
	return true
}
