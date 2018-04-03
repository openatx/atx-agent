package kexec

import (
	"log"
	"os"
	"os/exec"
	"strconv"
)

func Command(name string, arg ...string) *KCommand {
	return &KCommand{
		Cmd: exec.Command(name, arg...),
	}
}

func CommandString(command string) *KCommand {
	cmd := exec.Command("cmd", "/c", command)
	//cmd.Stdout = os.Stdout
	//cmd.Stderr = os.Stderr
	return &KCommand{
		Cmd: cmd,
	}
}

func (p *KCommand) Terminate(sig os.Signal) (err error) {
	if p.Process == nil {
		return nil
	}
	pid := p.Process.Pid
	c := exec.Command("taskkill", "/t", "/f", "/pid", strconv.Itoa(pid))
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// SetUser not support on windws
func (k *KCommand) SetUser(name string) (err error) {
	log.Printf("Can not set user(%s) on windows", name)
	return nil
}
