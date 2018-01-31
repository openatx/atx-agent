package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
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
	Args    []string
	Timeout time.Duration
	Shell   bool
}

func (c *Command) computedArgs() (name string, args []string) {
	if c.Shell {
		args = append(args, "-c", shellquote.Join(c.Args...))
		return "sh", args
	}
	return c.Args[0], c.Args[1:]
}

func (c Command) CombinedOutput() (output []byte, err error) {
	name, args := c.computedArgs()
	cmd := exec.Command(name, args...)
	var b bytes.Buffer
	cmd.Stdout = &b
	cmd.Stderr = &b
	if err = cmd.Start(); err != nil {
		return
	}
	if c.Timeout > 0 {
		timer := time.AfterFunc(c.Timeout, func() {
			cmd.Process.Kill()
		})
		defer timer.Stop()
	}
	err = cmd.Wait()
	return b.Bytes(), err
}

func (c Command) CombinedOutputString() (output string, err error) {
	bytesOutput, err := c.CombinedOutput()
	return string(bytesOutput), err
}

// need add timeout
func runShell(args ...string) (output []byte, err error) {
	return Command{
		Args:    args,
		Shell:   true,
		Timeout: 10 * time.Minute,
	}.CombinedOutput()
}

func runShellOutput(args ...string) (output []byte, err error) {
	return exec.Command("sh", "-c", strings.Join(args, " ")).Output()
}

func runShellTimeout(duration time.Duration, args ...string) (output []byte, err error) {
	return Command{
		Args:    args,
		Shell:   true,
		Timeout: duration,
	}.CombinedOutput()
}
