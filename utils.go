package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	shellquote "github.com/kballard/go-shellquote"
)

// TempFileName generates a temporary filename for use in testing or whatever
func TempFileName(dir, suffix string) string {
	randBytes := make([]byte, 16)
	rand.Read(randBytes)
	return filepath.Join(dir, hex.EncodeToString(randBytes)+suffix)
}

// Command add timeout support for os/exec
type Command struct {
	Args       []string
	Timeout    time.Duration
	Shell      bool
	ShellQuote bool
	Stdout     io.Writer
	Stderr     io.Writer
}

func (c *Command) computedArgs() (name string, args []string) {
	if c.Shell {
		var cmdline string
		if c.ShellQuote {
			cmdline = shellquote.Join(c.Args...)
		} else {
			cmdline = strings.Join(c.Args, " ") // simple, but works well with ">". eg Args("echo", "hello", ">output.txt")
		}
		args = append(args, "-c", cmdline)
		return "sh", args
	}
	return c.Args[0], c.Args[1:]
}

func (c Command) newCommand() *exec.Cmd {
	name, args := c.computedArgs()
	cmd := exec.Command(name, args...)
	if c.Stdout != nil {
		cmd.Stdout = c.Stdout
	}
	if c.Stderr != nil {
		cmd.Stderr = c.Stderr
	}
	return cmd
}

func (c Command) Run() error {
	cmd := c.newCommand()
	if c.Timeout > 0 {
		timer := time.AfterFunc(c.Timeout, func() {
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		})
		defer timer.Stop()
	}
	return cmd.Run()
}

func (c Command) Output() (output []byte, err error) {
	var b bytes.Buffer
	c.Stdout = &b
	c.Stderr = nil
	err = c.Run()
	return b.Bytes(), err
}

func (c Command) CombinedOutput() (output []byte, err error) {
	var b bytes.Buffer
	c.Stdout = &b
	c.Stderr = &b
	err = c.Run()
	return b.Bytes(), err
}

func (c Command) CombinedOutputString() (output string, err error) {
	bytesOutput, err := c.CombinedOutput()
	return string(bytesOutput), err
}

// need add timeout
func runShell(args ...string) (output []byte, err error) {
	return Command{
		Args:       args,
		Shell:      true,
		ShellQuote: false,
		Timeout:    10 * time.Minute,
	}.CombinedOutput()
}

func runShellOutput(args ...string) (output []byte, err error) {
	return Command{
		Args:       args,
		Shell:      true,
		ShellQuote: false,
		Timeout:    10 * time.Minute,
	}.Output()
}

func runShellTimeout(duration time.Duration, args ...string) (output []byte, err error) {
	return Command{
		Args:    args,
		Shell:   true,
		Timeout: duration,
	}.CombinedOutput()
}

type fakeWriter struct {
	writeFunc func([]byte) (int, error)
	Err       chan error
}

func (w *fakeWriter) Write(data []byte) (int, error) {
	n, err := w.writeFunc(data)
	if err != nil {
		select {
		case w.Err <- err:
		default:
		}
	}
	return n, err
}

func newFakeWriter(f func([]byte) (int, error)) *fakeWriter {
	return &fakeWriter{
		writeFunc: f,
		Err:       make(chan error, 1),
	}
}
