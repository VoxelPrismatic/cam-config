package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/term"

	"voxelprismatic/cam-config/cam"
)

func escalate() {
	var cmd *exec.Cmd
	if term.IsTerminal(0) {
		args := []string{"-E"}
		args = append(args, os.Args...)
		cmd = exec.Command("sudo", args...)
	} else {
		file, err := filepath.Abs(os.Args[0])
		if err != nil {
			panic(err)
		}
		os.Args[0] = file

		args := []string{"env"}
		for _, key := range os.Environ() {
			if !strings.Contains(key, " ") {
				args = append(args, key)
			}
		}

		args = append(args, os.Args...)
		cmd = exec.Command("pkexec", args...)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		panic(err)
	}
	os.Exit(0)
}

func main() {
	if syscall.Geteuid() != 0 {
		escalate()
		return
	}

	cam.RunGUI(os.Args)
}
