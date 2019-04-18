package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/codeskyblue/goreq"
	"github.com/codeskyblue/procfs"
	shellquote "github.com/kballard/go-shellquote"
	"github.com/openatx/androidutils"
	"github.com/pkg/errors"
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

// pidof
func pidOf(packageName string) (pid int, err error) {
	fs, err := procfs.NewFS(procfs.DefaultMountPoint)
	if err != nil {
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
	MainActivity string      `json:"mainActivity"`
	Label        string      `json:"label"`
	VersionName  string      `json:"versionName"`
	VersionCode  int         `json:"versionCode"`
	Size         int64       `json:"size"`
	Icon         image.Image `json:"-"`
}

func pkgInfo(packageName string) (info PackageInfo, err error) {
	outbyte, err := runShell("pm", "path", packageName)
	output := strings.TrimSpace(string(outbyte))
	if !strings.HasPrefix(output, "package:") {
		err = errors.New("package " + strconv.Quote(packageName) + " not found")
		return
	}
	apkpath := output[len("package:"):]
	finfo, err := os.Stat(apkpath)
	if err != nil {
		return
	}
	info.Size = finfo.Size()
	pkg, err := apk.OpenFile(apkpath)
	if err != nil {
		err = errors.Wrap(err, packageName)
		return
	}
	info.Label, _ = pkg.Label(nil)
	info.MainActivity, _ = pkg.MainActivity()
	info.Icon, _ = pkg.Icon(nil)
	info.VersionCode = pkg.Manifest().VersionCode
	info.VersionName = pkg.Manifest().VersionName
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

// Get preferred outbound ip of this machine
func GetOutboundIP() (net.IP, error) {
    conn, err := net.Dial("udp", "8.8.8.8:80")
    if err != nil {
        return nil, err
    }
    defer conn.Close()

    localAddr := conn.LocalAddr().(*net.UDPAddr)
    return localAddr.IP, nil
}

// Generate Addr with http:// and ws://
type NetAddr url.URL

// NewNetAddr accept http://.... or example.com or 192.168.0.1:3000
func NewNetAddr(addr string) *NetAddr {
	// var host string
	if !regexp.MustCompile(`^(http|ws)s?://`).MatchString(addr){
		addr = "http://"+addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		panic(err)
	}
	return (*NetAddr)(u)
}

func (n *NetAddr) HTTPAddr(paths ...string) string {
	return n.Scheme + "://"+n.Host + strings.Join(paths, "")
}

func (n *NetAddr) WebSocketAddr(paths ...string) string {
	scheme := "ws"
	if n.Scheme == "https" {
		scheme = "wss"
	}
	return scheme + "://"+n.Host+strings.Join(paths, "")
}