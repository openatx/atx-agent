// Like python-supervisor
// Manager process start, stop, restart
// Hope no bugs :)
package cmdctrl

import (
	"errors"
	"io"
	"log"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"
)

var debug = false

func debugPrintf(format string, v ...interface{}) {
	if debug {
		log.Printf("DEBUG "+format, v...)
	}
}

func goFunc(f func() error) chan error {
	errC := make(chan error, 1)
	go func() {
		errC <- f()
	}()
	return errC
}

type CommandInfo struct {
	Args            []string
	MaxRetries      int
	NextLaunchWait  time.Duration
	RecoverDuration time.Duration

	Stderr io.Writer
	Stdout io.Writer
	Stdin  io.Reader
}

type CommandCtrl struct {
	rl   sync.RWMutex
	cmds map[string]*processKeeper
}

func New() *CommandCtrl {
	return &CommandCtrl{
		cmds: make(map[string]*processKeeper, 10),
	}
}

func (cc *CommandCtrl) Exists(name string) bool {
	cc.rl.RLock()
	defer cc.rl.RUnlock()
	_, ok := cc.cmds[name]
	return ok
}

func (cc *CommandCtrl) Add(name string, c CommandInfo) error {
	if len(c.Args) == 0 {
		return errors.New("Args length must > 0")
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = 3
	}
	if c.RecoverDuration == 0 {
		c.RecoverDuration = 30 * time.Second
	}
	if c.NextLaunchWait == 0 {
		c.NextLaunchWait = 500 * time.Millisecond
	}

	cc.rl.Lock()
	defer cc.rl.Unlock()
	if _, exists := cc.cmds[name]; exists {
		return errors.New("name conflict: " + name)
	}
	cc.cmds[name] = &processKeeper{
		cmdInfo: c,
	}
	return nil
}

func (cc *CommandCtrl) Start(name string) error {
	cc.rl.RLock()
	defer cc.rl.RUnlock()
	pkeeper, ok := cc.cmds[name]
	if !ok {
		return errors.New("cmdctl not found: " + name)
	}
	return pkeeper.start()
}

// Stop send stop signal and quit immediately
func (cc *CommandCtrl) Stop(name string) error {
	cc.rl.RLock()
	defer cc.rl.RUnlock()
	pkeeper, ok := cc.cmds[name]
	if !ok {
		return errors.New("cmdctl not found: " + name)
	}
	return pkeeper.stop(false)
}

func (cc *CommandCtrl) Restart(name string) error {
	cc.rl.RLock()
	pkeeper, ok := cc.cmds[name]
	if !ok {
		cc.rl.RUnlock()
		return errors.New("cmdctl not found: " + name)
	}
	cc.rl.RUnlock()
	pkeeper.stop(true)
	return pkeeper.start()
}

// UpdateArgs func is not like exec.Command, the first argument name means cmdctl service name
// the seconds argument args, should like "echo", "hello"
// Example usage:
//   UpdateArgs("minitouch", "/data/local/tmp/minitouch", "-t", "1")
func (cc *CommandCtrl) UpdateArgs(name string, args ...string) error {
	cc.rl.RLock()
	defer cc.rl.RUnlock()
	if len(args) <= 0 {
		return errors.New("Args length must > 0")
	}
	pkeeper, ok := cc.cmds[name]
	if !ok {
		return errors.New("cmdctl not found: " + name)
	}
	pkeeper.cmdInfo.Args = args
	debugPrintf("cmd args: %v", pkeeper.cmdInfo.Args)
	if !pkeeper.keeping {
		return nil
	}
	return cc.Restart(name)
}

// keep process running
type processKeeper struct {
	mu         sync.Mutex
	cmdInfo    CommandInfo
	cmd        *exec.Cmd
	retries    int
	running    bool
	keeping    bool
	stopC      chan bool
	runBeganAt time.Time
	donewg     *sync.WaitGroup
}

// keep cmd running
func (p *processKeeper) start() error {
	p.mu.Lock()
	if p.keeping {
		p.mu.Unlock()
		return errors.New("already running")
	}
	p.keeping = true
	p.stopC = make(chan bool, 1)
	p.retries = 0
	p.donewg = &sync.WaitGroup{}
	p.donewg.Add(1)
	p.mu.Unlock()

	go func() {
		for {
			if p.retries < 0 {
				p.retries = 0
			}
			if p.retries > p.cmdInfo.MaxRetries {
				break
			}
			p.cmd = exec.Command(p.cmdInfo.Args[0], p.cmdInfo.Args[1:]...)
			p.cmd.Stdin = p.cmdInfo.Stdin
			p.cmd.Stdout = p.cmdInfo.Stdout
			p.cmd.Stderr = p.cmdInfo.Stderr
			debugPrintf("start")
			if err := p.cmd.Start(); err != nil {
				goto CMD_DONE
			}
			p.runBeganAt = time.Now()
			p.running = true
			cmdC := goFunc(p.cmd.Wait)
			select {
			case <-cmdC:
				if time.Since(p.runBeganAt) > p.cmdInfo.RecoverDuration {
					p.retries -= 2
				}
				p.retries++
				goto CMD_IDLE
			case <-p.stopC:
				p.terminate(cmdC)
				goto CMD_DONE
			}
		CMD_IDLE:
			debugPrintf("idle for %v", p.cmdInfo.NextLaunchWait)
			p.running = false
			select {
			case <-p.stopC:
				goto CMD_DONE
			case <-time.After(p.cmdInfo.NextLaunchWait):
				// do nothing
			}
		}
	CMD_DONE:
		p.mu.Lock()
		p.running = false
		p.keeping = false
		p.donewg.Done()
		p.mu.Unlock()
	}()
	return nil
}

var ErrAlreadyStopped = errors.New("already stopped")

func (p *processKeeper) terminate(cmdC chan error) {
	if runtime.GOOS == "windows" {
		if p.cmd.Process != nil {
			p.cmd.Process.Kill()
		}
		return
	}
	if p.cmd.Process != nil {
		p.cmd.Process.Signal(syscall.SIGTERM)
	}
	terminateWait := 3 * time.Second
	select {
	case <-cmdC:
		break
	case <-time.After(terminateWait):
		if p.cmd.Process != nil {
			p.cmd.Process.Kill()
		}
	}
	return
}

// stop cmd
func (p *processKeeper) stop(wait bool) error {
	p.mu.Lock()
	if !p.keeping {
		p.mu.Unlock()
		return ErrAlreadyStopped
	}
	select {
	case p.stopC <- true:
	default:
	}
	donewg := p.donewg // keep a copy of sync.WaitGroup
	p.mu.Unlock()

	if wait {
		donewg.Wait()
	}
	return nil
}
