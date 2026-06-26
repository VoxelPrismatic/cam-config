package cam

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const (
	logHi    = "\x1b[94;1m"
	logReset = "\x1b[0m"
)

func logRestart(reason string) {
	fmt.Fprintf(os.Stderr, "%s[cam-config] restarting capture: %s%s\n", logHi, reason, logReset)
}

func logCmdOutput(tool string, args []string, out string, err error) {
	line := tool + " " + strings.Join(args, " ")
	if text := strings.TrimSpace(out); text != "" {
		fmt.Fprintf(os.Stderr, "[%s] $ %s\n%s\n", tool, line, text)
	} else {
		fmt.Fprintf(os.Stderr, "[%s] $ %s\n", tool, line)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] error: %v\n", tool, err)
	}
}

func attachFFmpegLogs(cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	go streamLines("ffmpeg", stdout)
	go streamLines("ffmpeg", stderr)
	return nil
}

func streamLines(prefix string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fmt.Fprintf(os.Stderr, "[%s] %s\n", prefix, scanner.Text())
	}
}