// +build !windows

package kexec

import (
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

func setupCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Setsid = true
}

func Command(name string, arg ...string) *KCommand {
	cmd := exec.Command(name, arg...)
	setupCmd(cmd)
	return &KCommand{
		Cmd: cmd,
	}
}

func CommandString(command string) *KCommand {
	cmd := exec.Command("/bin/bash", "-c", command)
	setupCmd(cmd)
	//cmd.Stdout = os.Stdout
	//cmd.Stderr = os.Stderr
	return &KCommand{
		Cmd: cmd,
	}
}

func (p *KCommand) Terminate(sig os.Signal) (err error) {
	if p.Process == nil {
		return
	}
	// find pgid, ref: http://unix.stackexchange.com/questions/14815/process-descendants
	group, err := os.FindProcess(-1 * p.Process.Pid)
	//log.Println(group)
	if err == nil {
		err = group.Signal(sig)
	}
	return err
}

// Ref: http://stackoverflow.com/questions/21705950/running-external-commands-through-os-exec-under-another-user
func (k *KCommand) SetUser(name string) (err error) {
	u, err := user.Lookup(name)
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return err
	}
	if k.SysProcAttr == nil {
		k.SysProcAttr = &syscall.SysProcAttr{}
	}
	k.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	return nil
}
