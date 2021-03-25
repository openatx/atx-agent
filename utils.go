package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/codeskyblue/goreq"
	shellquote "github.com/kballard/go-shellquote"
	"github.com/openatx/androidutils"
	"github.com/pkg/errors"
	"github.com/prometheus/procfs"
	"github.com/shogo82148/androidbinary/apk"
)

// TempFileName generates a temporary filename for use in testing or whatever
func TempFileName(dir, suffix string) string {
	randBytes := make([]byte, 16)
	rand.Read(randBytes)
	return filepath.Join(dir, hex.EncodeToString(randBytes)+suffix)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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

func NewCommand(args ...string) *Command {
	return &Command{
		Args: args,
	}
}

func (c *Command) shellPath() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh, err := exec.LookPath("sh")
		if err == nil {
			return sh
		}
		sh = "/system/bin/sh"
	}
	return sh
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
		return c.shellPath(), args
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

func (c Command) StartBackground() (pid int, err error) {
	cmd := c.newCommand()
	err = cmd.Start()
	if err != nil {
		return
	}
	pid = cmd.Process.Pid
	return
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

type ProcInfo struct {
	Pid        int      `json:"pid"`
	PPid       int      `json:"ppid"`
	NumThreads int      `json:"threadCount"`
	Cmdline    []string `json:"cmdline"`
	Name       string   `json:"name"`
}

// Kill by Pid
func (p ProcInfo) Kill() error {
	process, err := os.FindProcess(p.Pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func listAllProcs() (ps []ProcInfo, err error) {
	fs, err := procfs.NewFS(procfs.DefaultMountPoint)
	if err != nil {
		return
	}
	procs, err := fs.AllProcs()
	if err != nil {
		return
	}
	for _, p := range procs {
		cmdline, _ := p.CmdLine()
		var name string
		if len(cmdline) == 1 {
			name = cmdline[0] // get package name
		} else {
			name, _ = p.Comm()
		}
		stat, _ := p.Stat()
		ps = append(ps, ProcInfo{
			Pid:        p.PID,
			PPid:       stat.PPID,
			Cmdline:    cmdline,
			Name:       name,
			NumThreads: stat.NumThreads,
		})
	}
	return
}

func findProcAll(packageName string) (procList []procfs.Proc, err error) {
	procs, err := procfs.AllProcs()
	for _, proc := range procs {
		cmdline, _ := proc.CmdLine()
		if len(cmdline) != 1 {
			continue
		}
		if cmdline[0] == packageName || strings.HasPrefix(cmdline[0], packageName+":") {
			procList = append(procList, proc)
		}
	}
	return
}

// pidof
func pidOf(packageName string) (pid int, err error) {
	fs, err := procfs.NewFS(procfs.DefaultMountPoint)
	if err != nil {
		return
	}
	// when packageName is int
	pid, er := strconv.Atoi(packageName)
	if er == nil {
		_, err = fs.NewProc(pid)
		return
	}
	procs, err := fs.AllProcs()
	if err != nil {
		return
	}
	for _, proc := range procs {
		cmdline, _ := proc.CmdLine()
		if len(cmdline) == 1 && cmdline[0] == packageName {
			return proc.PID, nil
		}
	}
	return 0, errors.New("package not found")
}

type PackageInfo struct {
	PackageName  string      `json:"packageName"`
	MainActivity string      `json:"mainActivity"`
	Label        string      `json:"label"`
	VersionName  string      `json:"versionName"`
	VersionCode  int         `json:"versionCode"`
	Size         int64       `json:"size"`
	Icon         image.Image `json:"-"`
}

func readPackageInfo(packageName string) (info PackageInfo, err error) {
	outbyte, err := runShell("pm", "path", packageName)
	output := strings.TrimSpace(string(outbyte))
	if !strings.HasPrefix(output, "package:") {
		err = errors.New("package " + strconv.Quote(packageName) + " not found")
		return
	}
	apkpath := output[len("package:"):]
	return readPackageInfoFromPath(apkpath)
}

func readPackageInfoFromPath(apkpath string) (info PackageInfo, err error) {
	finfo, err := os.Stat(apkpath)
	if err != nil {
		return
	}
	info.Size = finfo.Size()
	pkg, err := apk.OpenFile(apkpath)
	if err != nil {
		err = errors.Wrap(err, apkpath)
		return
	}
	defer pkg.Close()

	info.PackageName = pkg.PackageName()
	info.Label, _ = pkg.Label(nil)
	info.MainActivity, _ = pkg.MainActivity()
	info.Icon, _ = pkg.Icon(nil)
	info.VersionCode = int(pkg.Manifest().VersionCode.MustInt32())
	info.VersionName = pkg.Manifest().VersionName.MustString()
	return
}

func procWalk(fn func(p procfs.Proc)) error {
	fs, err := procfs.NewFS(procfs.DefaultMountPoint)
	if err != nil {
		return err
	}
	procs, err := fs.AllProcs()
	for _, proc := range procs {
		fn(proc)
	}
	return nil
}

// get main activity with packageName
func mainActivityOf(packageName string) (activity string, err error) {
	output, err := runShellOutput("pm", "list", "packages", "-f", packageName)
	if err != nil {
		log.Println("pm list err:", err)
		return
	}
	matches := regexp.MustCompile(`package:(.+)=([.\w]+)`).FindAllStringSubmatch(string(output), -1)
	for _, match := range matches {
		if match[2] != packageName {
			continue
		}
		pkg, err := apk.OpenFile(match[1])
		if err != nil {
			return "", err
		}
		return pkg.MainActivity()
	}
	return "", errors.New("package not found")
}

// download minicap or minitouch apk, etc...
func httpDownload(path string, urlStr string, perms os.FileMode) (written int64, err error) {
	resp, err := goreq.Request{
		Uri:             urlStr,
		RedirectHeaders: true,
		MaxRedirects:    10,
	}.Do()
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("http download <%s> status %v", urlStr, resp.Status)
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, perms)
	if err != nil {
		return
	}
	defer file.Close()
	written, err = io.Copy(file, resp.Body)
	log.Println("http download:", written)
	return
}

func hijackHTTPRequest(w http.ResponseWriter) (conn net.Conn, err error) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		err = errors.New("webserver don't support hijacking")
		return
	}

	hjconn, bufrw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	conn = newHijackReadWriteCloser(hjconn.(*net.TCPConn), bufrw)
	return
}

type hijactRW struct {
	*net.TCPConn
	bufrw *bufio.ReadWriter
}

func (this *hijactRW) Write(data []byte) (int, error) {
	nn, err := this.bufrw.Write(data)
	this.bufrw.Flush()
	return nn, err
}

func (this *hijactRW) Read(p []byte) (int, error) {
	return this.bufrw.Read(p)
}

func newHijackReadWriteCloser(conn *net.TCPConn, bufrw *bufio.ReadWriter) net.Conn {
	return &hijactRW{
		bufrw:   bufrw,
		TCPConn: conn,
	}
}

func getCachedProperty(name string) string {
	return androidutils.CachedProperty(name)
}

func getProperty(name string) string {
	return androidutils.Property(name)
}

func copyToFile(rd io.Reader, dst string) error {
	fd, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer fd.Close()
	_, err = io.Copy(fd, rd)
	return err
}

// parse output: dumpsys meminfo --local ${pkgname}
// If everything is going, returns json, unit KB
// {
//     "code": 58548,
//     "graphics": 73068,
//     "java heap": 160332,
//     "native heap": 67708,
//     "private Other": 34976,
//     "stack": 4728,
//     "system": 8288,
//     "total": 407648
// }
func parseMemoryInfo(nameOrPid string) (info map[string]int, err error) {
	output, err := Command{
		Args:    []string{"dumpsys", "meminfo", "--local", nameOrPid},
		Timeout: 10 * time.Second,
	}.CombinedOutputString()
	if err != nil {
		return
	}
	index := strings.Index(output, "App Summary")
	if index == -1 {
		err = errors.New("dumpsys meminfo has no [App Summary]")
		return
	}
	re := regexp.MustCompile(`(\w[\w ]+):\s*(\d+)`)
	matches := re.FindAllStringSubmatch(output[index:], -1)
	if len(matches) == 0 {
		err = errors.New("Invalid dumpsys meminfo output")
		return
	}
	info = make(map[string]int, len(matches))
	for _, m := range matches {
		key := strings.ToLower(m[1])
		val, _ := strconv.Atoi(m[2])
		info[key] = val
	}
	return
}

type CPUStat struct {
	Pid         int
	SystemTotal uint
	SystemIdle  uint
	ProcUser    uint
	ProcSystem  uint
	UpdateTime  time.Time
}

func NewCPUStat(pid int) (stat *CPUStat, err error) {
	stat = &CPUStat{Pid: pid}
	err = stat.Update()
	return
}

func (c *CPUStat) String() string {
	return fmt.Sprintf("CPUStat(pid:%d, systotal:%d, sysidle:%d, user:%d, system:%d",
		c.Pid, c.SystemTotal, c.SystemIdle, c.ProcUser, c.ProcSystem)
}

// CPUPercent may > 100.0 if process have multi thread on multi cores
func (c *CPUStat) CPUPercent(last *CPUStat) float64 {
	pjiff1 := last.ProcUser + last.ProcSystem
	pjiff2 := c.ProcUser + c.ProcSystem
	duration := c.SystemTotal - last.SystemTotal
	if duration <= 0 {
		return 0.0
	}
	percent := 100.0 * float64(pjiff2-pjiff1) / float64(duration)
	if percent < 0.0 {
		log.Println("Warning: cpu percent < 0", percent)
		percent = 0
	}
	return percent
}

func (c *CPUStat) SystemCPUPercent(last *CPUStat) float64 {
	idle := c.SystemIdle - last.SystemIdle
	jiff := c.SystemTotal - last.SystemTotal
	percent := 100.0 * float64(idle) / float64(jiff)
	return percent
}

// Update proc jiffies data
func (c *CPUStat) Update() error {
	// retrive /proc/<pid>/stat
	fs, err := procfs.NewFS(procfs.DefaultMountPoint)
	if err != nil {
		return err
	}
	proc, err := fs.NewProc(c.Pid)
	if err != nil {
		return err
	}
	stat, err := proc.NewStat()
	if err != nil {
		return errors.Wrap(err, "read /proc/<pid>/stat")
	}

	// retrive /proc/stst
	statData, err := ioutil.ReadFile("/proc/stat")
	if err != nil {
		return errors.Wrap(err, "read /proc/stat")
	}
	procStat := string(statData)
	idx := strings.Index(procStat, "\n")
	// cpuName, user, nice, system, idle, iowait, irq, softIrq, steal, guest, guestNice
	fields := strings.Fields(procStat[:idx])
	if fields[0] != "cpu" {
		return errors.New("/proc/stat not startswith cpu")
	}
	var total, idle uint
	for i, raw := range fields[1:] {
		var v uint
		fmt.Sscanf(raw, "%d", &v)
		if i == 3 { // idle
			idle = v
		}
		total += v
	}

	c.ProcSystem = stat.STime
	c.ProcUser = stat.UTime
	c.SystemTotal = total
	c.SystemIdle = idle
	c.UpdateTime = time.Now()
	return nil
}

var cpuStats = make(map[int]*CPUStat)

var _cpuCoreCount int

// CPUCoreCount return 0 if retrive failed
func CPUCoreCount() int {
	if _cpuCoreCount != 0 {
		return _cpuCoreCount
	}
	fs, err := procfs.NewFS(procfs.DefaultMountPoint)
	if err != nil {
		return 0
	}
	stat, err := fs.NewStat()
	if err != nil {
		return 0
	}
	_cpuCoreCount = len(stat.CPU)
	return _cpuCoreCount
}

type CPUInfo struct {
	Pid           int     `json:"pid"`
	User          uint    `json:"user"`
	System        uint    `json:"system"`
	Percent       float64 `json:"percent"`
	SystemPercent float64 `json:"systemPercent"`
	CoreCount     int     `json:"coreCount"`
}

func readCPUInfo(pid int) (info CPUInfo, err error) {
	last, ok := cpuStats[pid]
	if !ok || // need fresh history data
		last.UpdateTime.Add(5*time.Second).Before(time.Now()) {
		last, err = NewCPUStat(pid)
		if err != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
		log.Println("Update data")
	}
	stat, err := NewCPUStat(pid)
	if err != nil {
		return
	}
	cpuStats[pid] = stat
	info.Pid = pid
	info.User = stat.ProcUser
	info.System = stat.ProcSystem
	info.Percent = stat.CPUPercent(last)
	info.SystemPercent = stat.SystemCPUPercent(last)
	info.CoreCount = CPUCoreCount()
	return
}

func dumpHierarchy() (xmlContent string, err error) {
	var targetPath = filepath.Join(expath, "window_dump.xml")
	c := &Command{
		Args:  []string{"uiautomator", "dump", targetPath},
		Shell: true,
	}
	if err = c.Run(); err != nil {
		return
	}
	data, err := ioutil.ReadFile(targetPath)
	xmlContent = string(data)
	return
}

func listPackages() (pkgs []PackageInfo, err error) {
	c := NewCommand("pm", "list", "packages", "-f", "-3")
	c.Shell = true
	output, err := c.CombinedOutputString()
	if err != nil {
		return
	}
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "package:") {
			continue
		}
		matches := regexp.MustCompile(`^package:(/.+)=([^=]+)$`).FindStringSubmatch(line)
		if len(matches) == 0 {
			continue
		}
		pkgPath := matches[1]
		pkgName := matches[2]
		pkgInfo, er := readPackageInfoFromPath(pkgPath)
		if er != nil {
			log.Printf("Read package %s error %v", pkgName, er)
			continue
		}
		pkgs = append(pkgs, pkgInfo)
	}
	return
}

func killProcessByName(processName string) bool {
	procs, err := procfs.AllProcs()
	if err != nil {
		return false
	}

	killed := false
	for _, p := range procs {
		cmdline, _ := p.CmdLine()
		var name string
		if len(cmdline) >= 1 {
			name = filepath.Base(cmdline[0])
		} else {
			name, _ = p.Comm()
		}

		if name == processName {
			process, err := os.FindProcess(p.PID)
			if err == nil {
				process.Kill()
				killed = true
			}
		}
	}
	return killed
}

func getPackagePath(packageName string) (string, error) {
	pmPathOutput, err := Command{
		Args:  []string{"pm", "path", "com.github.uiautomator"},
		Shell: true,
	}.CombinedOutputString()
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(pmPathOutput, "package:") {
		return "", errors.New("invalid pm path output: " + pmPathOutput)
	}
	packagePath := strings.TrimSpace(pmPathOutput[len("package:"):])
	return packagePath, nil
}
