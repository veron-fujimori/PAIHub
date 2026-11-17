package sysenv

import (
	"context"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
)

func KillProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	}
	_ = cmd.Process.Kill()
}

func StreamCommand(
	ctx context.Context,
	cmd *exec.Cmd,
	onStarted func(*exec.Cmd) error,
	transform func(text string) string,
	onChunk func(stream, text string),
) (int, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return -1, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return -1, err
	}

	if err := cmd.Start(); err != nil {
		return -1, err
	}
	if onStarted != nil {
		if err := onStarted(cmd); err != nil {
			KillProcessTree(cmd)
			_ = cmd.Wait()
			return -1, err
		}
	}

	var cbMu sync.Mutex
	emit := func(stream, text string) {
		if text == "" || onChunk == nil {
			return
		}
		if transform != nil {
			text = transform(text)
			if text == "" {
				return
			}
		}
		cbMu.Lock()
		onChunk(stream, text)
		cbMu.Unlock()
	}

	var wg sync.WaitGroup
	readPipe := func(name string, rd io.Reader) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, rErr := rd.Read(buf)
			if n > 0 {
				emit(name, string(buf[:n]))
			}
			if rErr != nil {
				break
			}
		}
	}

	wg.Add(2)
	go readPipe("stdout", stdout)
	go readPipe("stderr", stderr)

	doneCh := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			KillProcessTree(cmd)
		case <-doneCh:
		}
	}()

	wg.Wait()
	waitErr := cmd.Wait()
	close(doneCh)

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	return exitCode, waitErr
}
