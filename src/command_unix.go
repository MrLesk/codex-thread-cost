//go:build darwin || linux

package main

import "os/exec"

func command(binary string, args []string) (*exec.Cmd, error) {
	return exec.Command(binary, args...), nil
}
