package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	syslog "log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin"
	"github.com/dustin/go-broadcast"
	"github.com/gorilla/websocket"
	"github.com/openatx/androidutils"
	"github.com/openatx/atx-agent/cmdctrl"
	"github.com/openatx/atx-agent/subcmd"
	"github.com/pkg/errors"
	"github.com/qiniu/log"
	"github.com/sevlyar/go-daemon"
)

var (
	service     = cmdctrl.New()
	downManager = newDownloadManager()
	upgrader    = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	version       = "dev"
	owner         = "openatx"
	repo          = "atx-agent"
	listenPort    int
	daemonLogPath = "/sdcard/atx-agent.log"

	rotationPublisher   = broadcast.NewBroadcaster(1)
	minicapSocketPath   = "@minicap"
	minitouchSocketPath = "@minitouch"
)

const (
	apkVersionCode = 4
	apkVersionName = "1.0.4"
)

func init() {
	syslog.SetFlags(syslog.Lshortfile | syslog.LstdFlags)
}

// singleFight for http request
// - minicap
// - minitouch
var muxMutex = sync.Mutex{}
var muxLocks = make(map[string]bool)
var muxConns = make(map[string]*websocket.Conn)

func singleFightWrap(handleFunc func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		muxMutex.Lock()
		if _, ok := muxLocks[r.RequestURI]; ok {
			muxMutex.Unlock()
			log.Println("singlefight conflict", r.RequestURI)
			http.Error(w, "singlefight conflicts", http.StatusTooManyRequests) // code: 429
			return
		}
		muxLocks[r.RequestURI] = true
		muxMutex.Unlock()

		handleFunc(w, r) // handle requests

		muxMutex.Lock()
		delete(muxLocks, r.RequestURI)
		muxMutex.Unlock()
	}
}

func singleFightNewerWebsocket(handleFunc func(http.ResponseWriter, *http.Request, *websocket.Conn)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		muxMutex.Lock()
		if oldWs, ok := muxConns[r.RequestURI]; ok {
			oldWs.Close()
			delete(muxConns, r.RequestURI)
		}

		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			http.Error(w, "websocket upgrade error", 500)
			muxMutex.Unlock()
			return
		}
		muxConns[r.RequestURI] = wsConn
		muxMutex.Unlock()

		handleFunc(w, r, wsConn) // handle request

		muxMutex.Lock()
		if muxConns[r.RequestURI] == wsConn { // release connection
			delete(muxConns, r.RequestURI)
		}
		muxMutex.Unlock()
	}
}

// Get preferred outbound ip of this machine
func getOutboundIP() (ip net.IP, err error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP, nil
}

func mustGetOoutboundIP() net.IP {
	ip, err := getOutboundIP()
	if err != nil {
		return net.ParseIP("127.0.0.1")
		// panic(err)
	}
	return ip
}

func renderJSON(w http.ResponseWriter, data interface{}) {
	js, err := json.Marshal(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(js)))
	w.Write(js)
}

func cmdError2Code(err error) int {
	if err == nil {
		return 0
	}
	if exiterr, ok := err.(*exec.ExitError); ok {
		// The program has exited with an exit code != 0

		// This works on both Unix and Windows. Although package
		// syscall is generally platform dependent, WaitStatus is
		// defined for both Unix and Windows and in both cases has
		// an ExitStatus() method with the same signature.
		if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	return 128
}

func GoFunc(f func() error) chan error {
	ch := make(chan error)
	go func() {
		ch <- f()
	}()
	return ch
}

type MinicapInfo struct {
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Rotation int     `json:"rotation"`
	Density  float32 `json:"density"`
}

var (
	deviceRotation        int
	displayMaxWidthHeight = 800
)

func updateMinicapRotation(rotation int) {
	running := service.Running("minicap")
	if running {
		service.Stop("minicap")
		killProcessByName("minicap") // kill not controlled minicap
	}
	devInfo := getDeviceInfo()
	width, height := devInfo.Display.Width, devInfo.Display.Height
	service.UpdateArgs("minicap", "/data/local/tmp/minicap", "-S", "-P",
		fmt.Sprintf("%dx%d@%dx%d/%d", width, height, displayMaxWidthHeight, displayMaxWidthHeight, rotation))
	if running {
		service.Start("minicap")
	}
}

func checkUiautomatorInstalled() (ok bool) {
	pi, err := androidutils.StatPackage("com.github.uiautomator")
	if err != nil {
		return
	}
	if pi.Version.Code < apkVersionCode {
		return
	}
	_, err = androidutils.StatPackage("com.github.uiautomator.test")
	return err == nil
}

type DownloadManager struct {
	db map[string]*downloadProxy
	mu sync.Mutex
	n  int
}

func newDownloadManager() *DownloadManager {
	return &DownloadManager{
		db: make(map[string]*downloadProxy, 10),
	}
}

func (m *DownloadManager) Get(id string) *downloadProxy {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.db[id]
}

func (m *DownloadManager) Put(di *downloadProxy) (id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n += 1
	id = strconv.Itoa(m.n)
	m.db[id] = di
	// di.Id = id
	return id
}

func (m *DownloadManager) Del(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.db, id)
}

func (m *DownloadManager) DelayDel(id string, sleep time.Duration) {
	go func() {
		time.Sleep(sleep)
		m.Del(id)
	}()
}

func currentUserName() string {
	if u, err := user.Current(); err == nil {
		return u.Name
	}
	if name := os.Getenv("USER"); name != "" {
		return name
	}
	output, err := exec.Command("whoami").Output()
	if err == nil {
		return strings.TrimSpace(string(output))
	}
	return ""
}

func renderHTML(w http.ResponseWriter, filename string) {
	file, err := Assets.Open(filename)
	if err != nil {
		http.Error(w, "404 page not found", 404)
		return
	}
	content, _ := ioutil.ReadAll(file)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Write(content)
}

var (
	ErrJpegWrongFormat = errors.New("jpeg format error, not starts with 0xff,0xd8")

	// target, _ := url.Parse("http://127.0.0.1:9008")
	// uiautomatorProxy := httputil.NewSingleHostReverseProxy(target)

	uiautomatorTimer = NewSafeTimer(time.Minute * 3)

	uiautomatorProxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.RawQuery = "" // ignore http query
			req.URL.Scheme = "http"
			req.URL.Host = "127.0.0.1:9008"

			if req.URL.Path == "/jsonrpc/0" {
				uiautomatorTimer.Reset()
			}
		},
		Transport: &http.Transport{
			// Ref: https://golang.org/pkg/net/http/#RoundTripper
			Dial: func(network, addr string) (net.Conn, error) {
				conn, err := (&net.Dialer{
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
					DualStack: true,
				}).Dial(network, addr)
				return conn, err
			},
			MaxIdleConns:          100,
			IdleConnTimeout:       180 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
)

type errorBinaryReader struct {
	rd  io.Reader
	err error
}

func (r *errorBinaryReader) ReadInto(datas ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	for _, data := range datas {
		r.err = binary.Read(r.rd, binary.LittleEndian, data)
		if r.err != nil {
			return r.err
		}
	}
	return nil
}

// read from @minicap and send jpeg raw data to channel
func translateMinicap(conn net.Conn, jpgC chan []byte, ctx context.Context) error {
	var pid, rw, rh, vw, vh uint32
	var version, unused, orientation, quirkFlag uint8
	rd := bufio.NewReader(conn)
	binRd := errorBinaryReader{rd: rd}
	err := binRd.ReadInto(&version, &unused, &pid, &rw, &rh, &vw, &vh, &orientation, &quirkFlag)
	if err != nil {
		return err
	}
	for {
		var size uint32
		if err = binRd.ReadInto(&size); err != nil {
			break
		}

		lr := &io.LimitedReader{R: rd, N: int64(size)}
		buf := bytes.NewBuffer(nil)
		_, err = io.Copy(buf, lr)
		if err != nil {
			break
		}
		if string(buf.Bytes()[:2]) != "\xff\xd8" {
			err = ErrJpegWrongFormat
			break
		}
		select {
		case jpgC <- buf.Bytes(): // Maybe should use buffer instead
		case <-ctx.Done():
			return nil
		default:
			// TODO(ssx): image should not wait or it will stuck here
		}
	}
	return err
}

func runDaemon() (cntxt *daemon.Context) {
	cntxt = &daemon.Context{ // remove pid to prevent resource busy
		PidFilePerm: 0644,
		LogFileName: daemonLogPath,
		LogFilePerm: 0640,
		WorkDir:     "./",
		Umask:       022,
	}
	child, err := cntxt.Reborn()
	if err != nil {
		log.Fatal("Unale to run: ", err)
	}
	if child != nil {
		return nil // return nil indicate program run in parent
	}
	return cntxt
}

func stopSelf() {
	// kill previous daemon first
	log.Println("stop server self")

	client := http.Client{Timeout: 3 * time.Second}
	_, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/stop", listenPort))
	if err == nil {
		log.Println("wait server stopped")
		time.Sleep(500 * time.Millisecond) // server will quit in 0.5s
	} else {
		log.Println("already stopped")
	}

	// to make sure stopped
	killAgentProcess()
}

func init() {
	// Set timezone.
	//
	// Note that Android zoneinfo is stored in /system/usr/share/zoneinfo,
	// but it is in some kind of packed TZiff file that we do not support
	// yet. To make it simple, we use FixedZone instead
	zones := map[string]int{
		"Asia/Shanghai": 8,
		"CST":           8, // China Standard Time
	}
	tz := getCachedProperty("persist.sys.timezone")
	if tz != "" {
		offset, ok := zones[tz]
		if !ok {
			// get offset from date command, example date output: +0800\n
			output, _ := runShell("date", "+%z")
			if len(output) != 6 {
				return
			}
			offset, _ = strconv.Atoi(string(output[1:3]))
			if output[0] == '-' {
				offset *= -1
			}
		}
		time.Local = time.FixedZone(tz, offset*3600)
	}

	// watch rotation and send to rotatinPublisher
	go _watchRotation()
	if !isMinicapSupported() {
		minicapSocketPath = "@minicapagent"
	}
	if !fileExists("/data/local/tmp/minitouch") {
		minitouchSocketPath = "@minitouchagent"
	} else if sdk, _ := strconv.Atoi(getCachedProperty("ro.build.version.sdk")); sdk > 28 { // Android Q..
		minitouchSocketPath = "@minitouchagent"
	}
}

func _watchRotation() {
	for {
		conn, err := net.Dial("unix", "@rotationagent")
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		func() {
			defer conn.Close()
			scanner := bufio.NewScanner(conn)
			for scanner.Scan() {
				rotation, err := strconv.Atoi(scanner.Text())
				if err != nil {
					continue
				}
				deviceRotation = rotation
				if minicapSocketPath == "@minicap" {
					updateMinicapRotation(deviceRotation)
				}
				rotationPublisher.Submit(rotation)
				log.Println("Rotation -->", rotation)
			}
		}()
		time.Sleep(1 * time.Second)
	}
}

func killAgentProcess() error {
	// kill process by process cmdline
	procs, err := listAllProcs()
	if err != nil {
		return err
	}
	for _, p := range procs {
		if os.Getpid() == p.Pid {
			// do not kill self
			continue
		}
		if len(p.Cmdline) >= 2 {
			// cmdline: /data/local/tmp/atx-agent server -d
			if filepath.Base(p.Cmdline[0]) == "atx-agent" && p.Cmdline[1] == "server" {
				log.Infof("kill running atx-agent (pid=%d)", p.Pid)
				p.Kill()
			}
		}
	}
	return nil
}

func main() {
	kingpin.Version(version)
	kingpin.CommandLine.HelpFlag.Short('h')
	kingpin.CommandLine.VersionFlag.Short('v')

	// CMD: curl
	cmdCurl := kingpin.Command("curl", "curl command")
	subcmd.RegisterCurl(cmdCurl)

	// CMD: server
	cmdServer := kingpin.Command("server", "start server")
	fDaemon := cmdServer.Flag("daemon", "daemon mode").Short('d').Bool()
	fStop := cmdServer.Flag("stop", "stop server").Bool()
	cmdServer.Flag("port", "listen port").Default("7912").Short('p').IntVar(&listenPort) // Create on 2017/09/12
	cmdServer.Flag("log", "log file path when in daemon mode").StringVar(&daemonLogPath)
	fServerURL := cmdServer.Flag("server", "server url").Short('t').String()
	fNoUiautomator := cmdServer.Flag("nouia", "do not start uiautoamtor when start").Bool()

	// CMD: version
	kingpin.Command("version", "show version")

	// CMD: install
	cmdIns := kingpin.Command("install", "install apk")
	apkStart := cmdIns.Flag("start", "start when installed").Short('s').Bool()
	apkPath := cmdIns.Arg("apkPath", "apk path").Required().String()

	// CMD: info
	os.Setenv("COLUMNS", "160")

	kingpin.Command("info", "show device info")
	switch kingpin.Parse() {
	case "curl":
		subcmd.DoCurl()
		return
	case "version":
		println(version)
		return
	case "install":
		am := &APKManager{Path: *apkPath}
		if err := am.ForceInstall(); err != nil {
			log.Fatal(err)
		}
		if *apkStart {
			am.Start(StartOptions{})
		}
		return
	case "info":
		data, _ := json.MarshalIndent(getDeviceInfo(), "", "  ")
		println(string(data))
		return
	case "server":
		// continue
	}

	if *fStop {
		stopSelf()
		if !*fDaemon {
			return
		}
	}

	// if *fRequirements {
	// 	log.Println("check dependencies")
	// 	if err := installRequirements(); err != nil {
	// 		// panic(err)
	// 		log.Println("requirements not ready:", err)
	// 		return
	// 	}
	// }

	serverURL := *fServerURL
	if serverURL != "" {
		if !regexp.MustCompile(`https?://`).MatchString(serverURL) {
			serverURL = "http://" + serverURL
		}
		u, err := url.Parse(serverURL)
		if err != nil {
			log.Fatal(err)
		}
		_ = u
	}

	if _, err := os.Stat("/sdcard/tmp"); err != nil {
		os.MkdirAll("/sdcard/tmp", 0755)
	}
	os.Setenv("TMPDIR", "/sdcard/tmp")

	if *fDaemon {
		log.Println("run atx-agent in background")

		cntxt := runDaemon()
		if cntxt == nil {
			log.Printf("atx-agent listening on %v:%d", mustGetOoutboundIP(), listenPort)
			return
		}
		defer cntxt.Release()
		log.Print("- - - - - - - - - - - - - - -")
		log.Print("daemon started")
	}

	fmt.Printf("atx-agent version %s\n", version)

	// show ip
	outIp, err := getOutboundIP()
	if err == nil {
		fmt.Printf("Listen on http://%v:%d\n", outIp, listenPort)
	} else {
		fmt.Printf("Internet is not connected.")
	}

	listener, err := net.Listen("tcp", ":"+strconv.Itoa(listenPort))
	if err != nil {
		log.Fatal(err)
	}

	// minicap + minitouch
	devInfo := getDeviceInfo()

	width, height := devInfo.Display.Width, devInfo.Display.Height
	service.Add("minicap", cmdctrl.CommandInfo{
		Environ: []string{"LD_LIBRARY_PATH=/data/local/tmp"},
		Args: []string{"/data/local/tmp/minicap", "-S", "-P",
			fmt.Sprintf("%dx%d@%dx%d/0", width, height, displayMaxWidthHeight, displayMaxWidthHeight)},
	})

	service.Add("apkagent", cmdctrl.CommandInfo{
		MaxRetries: 2,
		Shell:      true,
		OnStart: func() error {
			log.Println("killProcessByName apk-agent.cli")
			killProcessByName("apkagent.cli")
			return nil
		},
		ArgsFunc: func() ([]string, error) {
			packagePath, err := getPackagePath("com.github.uiautomator")
			if err != nil {
				return nil, err
			}
			return []string{"CLASSPATH=" + packagePath, "exec", "app_process", "/system/bin", "com.github.uiautomator.Console"}, nil
		},
	})

	service.Start("apkagent")

	service.Add("minitouch", cmdctrl.CommandInfo{
		MaxRetries: 2,
		Args:       []string{"/data/local/tmp/minitouch"},
		Shell:      true,
	})

	// uiautomator 1.0
	service.Add("uiautomator-1.0", cmdctrl.CommandInfo{
		Args: []string{"sh", "-c",
			"uiautomator runtest uiautomator-stub.jar bundle.jar -c com.github.uiautomatorstub.Stub"},
		// Args: []string{"uiautomator", "runtest", "/data/local/tmp/uiautomator-stub.jar", "bundle.jar","-c", "com.github.uiautomatorstub.Stub"},
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		MaxRetries:      3,
		RecoverDuration: 30 * time.Second,
		StopSignal:      os.Interrupt,
		OnStart: func() error {
			uiautomatorTimer.Reset()
			return nil
		},
		OnStop: func() {
			uiautomatorTimer.Stop()
		},
	})

	// uiautomator 2.0
	service.Add("uiautomator", cmdctrl.CommandInfo{
		Args: []string{"am", "instrument", "-w", "-r",
			"-e", "debug", "false",
			"-e", "class", "com.github.uiautomator.stub.Stub",
			"com.github.uiautomator.test/android.support.test.runner.AndroidJUnitRunner"},
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		MaxRetries:      1, // only once
		RecoverDuration: 30 * time.Second,
		StopSignal:      os.Interrupt,
		OnStart: func() error {
			uiautomatorTimer.Reset()
			// log.Println("service uiautomator: startservice com.github.uiautomator/.Service")
			// runShell("am", "startservice", "-n", "com.github.uiautomator/.Service")
			return nil
		},
		OnStop: func() {
			uiautomatorTimer.Stop()
			// log.Println("service uiautomator: stopservice com.github.uiautomator/.Service")
			// runShell("am", "stopservice", "-n", "com.github.uiautomator/.Service")
			// runShell("am", "force-stop", "com.github.uiautomator")
		},
	})

	// stop uiautomator when 3 minutes not requests
	go func() {
		for range uiautomatorTimer.C {
			log.Println("uiautomator has not activity for 3 minutes, closed")
			service.Stop("uiautomator")
			service.Stop("uiautomator-1.0")
		}
	}()

	if !*fNoUiautomator {
		if err := service.Start("uiautomator"); err != nil {
			log.Println("uiautomator start failed:", err)
		}
	}

	server := NewServer()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range sigc {
			needStop := false
			switch sig {
			case syscall.SIGTERM:
				needStop = true
			case syscall.SIGHUP:
			case syscall.SIGINT:
				if !*fDaemon {
					needStop = true
				}
			}
			if needStop {
				log.Println("Catch signal", sig)
				service.StopAll()
				server.httpServer.Shutdown(context.TODO())
				return
			}
			log.Println("Ignore signal", sig)
		}
	}()

	service.Start("minitouch")

	// run server forever
	if err := server.Serve(listener); err != nil {
		log.Println("server quit:", err)
	}
}
