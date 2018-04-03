package kexec

import (
	"errors"
	"os/exec"
	"sync"
)

type KCommand struct {
	*exec.Cmd

	errCs    []chan error
	err      error
	finished bool
	once     sync.Once
	mu       sync.Mutex
}

func (c *KCommand) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

// This Wait wraps exec.Wait, but support multi call
func (k *KCommand) Wait() error {
	if k.Process == nil {
		return errors.New("exec: not started")
	}
	k.once.Do(func() {
		if k.errCs == nil {
			k.errCs = make([]chan error, 0)
		}
		go func() {
			k.err = k.Cmd.Wait()
			k.mu.Lock()
			k.finished = true
			for _, errC := range k.errCs {
				errC <- k.err
			}
			k.mu.Unlock()
		}()
	})
	k.mu.Lock()
	if k.finished {
		k.mu.Unlock()
		return k.err
	}
	errC := make(chan error, 1)
	k.errCs = append(k.errCs, errC)
	k.mu.Unlock()
	return <-errC
}
