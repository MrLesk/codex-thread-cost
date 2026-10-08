package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func command(binary string, args []string) (*exec.Cmd, error) {
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(resolved)
	if !strings.HasSuffix(lower, ".cmd") && !strings.HasSuffix(lower, ".bat") {
		return exec.Command(resolved, args...), nil
	}
	// npm distributes Codex as a cmd shim. Quote every argument for cmd.exe.
	all := append([]string{resolved}, args...)
	for i, arg := range all {
		if strings.ContainsAny(arg, "\"%\r\n") {
			return nil, fmt.Errorf("use a native codex.exe path for this configuration")
		}
		all[i] = "\"" + arg + "\""
	}
	shell := os.Getenv("COMSPEC")
	if shell == "" {
		shell = "cmd.exe"
	}
	cmd := exec.Command(shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd.exe /d /s /c \"" + strings.Join(all, " ") + "\""}
	return cmd, nil
}
