// Like python-supervisor
// Manager process start, stop, restart
// Hope no bugs :)
package cmdctrl

import (
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"
)

var (
	debug = true

	ErrAlreadyRunning = errors.New("already running")
	ErrAlreadyStopped = errors.New("already stopped")
)

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
	Environ         []string
	Args            []string
	MaxRetries      int           // 3
	NextLaunchWait  time.Duration // 0.5s
	RecoverDuration time.Duration // 30s

	OnStart func() error // if return non nil, cmd will not run
	OnStop  func()

	Stderr io.Writer // nil
	Stdout io.Writer // nil
	Stdin  io.Reader // nil
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
	if pkeeper.cmdInfo.OnStart != nil {
		if err := pkeeper.cmdInfo.OnStart(); err != nil {
			return err
		}
	}
	return pkeeper.start()
}

// Stop send stop signal
// Stop("demo") will quit immediately
// Stop("demo", true) will quit until command really killed
func (cc *CommandCtrl) Stop(name string, waits ...bool) error {
	cc.rl.RLock()
	defer cc.rl.RUnlock()
	pkeeper, ok := cc.cmds[name]
	if !ok {
		return errors.New("cmdctl not found: " + name)
	}
	wait := false
	if len(waits) > 0 {
		wait = waits[0]
	}
	return pkeeper.stop(wait)
}

// StopAll command and wait until all program quited
func (cc *CommandCtrl) StopAll() {
	for _, pkeeper := range cc.cmds {
		pkeeper.stop(true)
	}
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

// Running return bool indicate if program is still running
func (cc *CommandCtrl) Running(name string) bool {
	cc.rl.RLock()
	defer cc.rl.RUnlock()
	pkeeper, ok := cc.cmds[name]
	if !ok {
		return false
	}
	return pkeeper.keeping
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
		return ErrAlreadyRunning
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
			p.cmd.Env = append(os.Environ(), p.cmdInfo.Environ...)
			p.cmd.Stdin = p.cmdInfo.Stdin
			p.cmd.Stdout = p.cmdInfo.Stdout
			p.cmd.Stderr = p.cmdInfo.Stderr
			debugPrintf("start args: %v, env: %v", p.cmdInfo.Args, p.cmdInfo.Environ)
			if err := p.cmd.Start(); err != nil {
				goto CMD_DONE
			}
			debugPrintf("program pid: %d", p.cmd.Process.Pid)
			p.runBeganAt = time.Now()
			p.running = true
			cmdC := goFunc(p.cmd.Wait)
			select {
			case cmdErr := <-cmdC:
				debugPrintf("cmd wait err: %v", cmdErr)
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
		debugPrintf("program finished")
		if p.cmdInfo.OnStop != nil {
			p.cmdInfo.OnStop()
		}
		p.mu.Lock()
		p.running = false
		p.keeping = false
		p.donewg.Done()
		p.mu.Unlock()
	}()
	return nil
}

// TODO: support kill by env, like jenkins
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
