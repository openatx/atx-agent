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
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin"
	"github.com/dustin/go-broadcast"
	"github.com/gorilla/websocket"
	"github.com/openatx/atx-agent/cmdctrl"
	"github.com/openatx/atx-agent/logger"
	"github.com/openatx/atx-agent/subcmd"
	"github.com/pkg/errors"
	"github.com/sevlyar/go-daemon"
)

var (
	expath, _   = GetExDir()
	service     = cmdctrl.New()
	downManager = newDownloadManager()
	upgrader    = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	version       = "v2.0.7"
	owner         = "dolfly"
	repo          = "atx-agent"
	listenAddr    string
	daemonLogPath = filepath.Join(expath, "atx-agent.daemon.log")

	rotationPublisher   = broadcast.NewBroadcaster(1)
	minicapSocketPath   = "@minicap"
	minitouchSocketPath = "@minitouch"
	log                 = logger.Default
)

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

// Get executable directory based on current running binary
func GetExDir() (dir string, err error) {
	ex, err := os.Executable()
	if err != nil {
		log.Println("Failed to get executable directory.")
		return "", err
	}
	dir = filepath.Dir(ex)
	return dir, err
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
	service.UpdateArgs("minicap", fmt.Sprintf("%v/%v", expath, "minicap"), "-S", "-P",
		fmt.Sprintf("%dx%d@%dx%d/%d", width, height, displayMaxWidthHeight, displayMaxWidthHeight, rotation))
	if running {
		service.Start("minicap")
	}
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

	uiautomatorTimer = NewSafeTimer(time.Hour * 3)

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
		LogFilePerm: 0640,
		LogFileName: filepath.Join(expath, "atx-agent.log"),
		WorkDir:     "./",
		Umask:       022,
	}

	// log might be no auth
	if f, err := os.OpenFile(daemonLogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil { // |os.O_APPEND
		f.Close()
		cntxt.LogFileName = daemonLogPath
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

func setupLogrotate() {
	logger.SetOutputFile("/sdcard/atx-agent.log")
}

func stopSelf() {
	// kill previous daemon first
	log.Println("stop server self")

	listenPort, _ := strconv.Atoi(strings.Split(listenAddr, ":")[1])
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
	syslog.SetFlags(syslog.Lshortfile | syslog.LstdFlags)

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
}

// lazyInit will be called in func:main
func lazyInit() {
	// watch rotation and send to rotatinPublisher
	go _watchRotation()
	if !isMinicapSupported() {
		minicapSocketPath = "@minicapagent"
	}
	if !fileExists(path.Join(expath, "minitouch")) {
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
			if filepath.Base(p.Cmdline[0]) == filepath.Base(os.Args[0]) && p.Cmdline[1] == "server" {
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

	// CMD: frpc
	cmdFrpc := kingpin.Command("frpc", "frpc command")
	subcmd.RegisterFrpc(cmdFrpc)

	// CMD: server
	cmdServer := kingpin.Command("server", "start server")
	fDaemon := cmdServer.Flag("daemon", "daemon mode").Short('d').Bool()
	fStop := cmdServer.Flag("stop", "stop server").Bool()

	cmdServer.Flag("addr", "listen addr").Default(":7912").StringVar(&listenAddr) // Create on 2017/09/12
	cmdServer.Flag("log", "log file path when in daemon mode").StringVar(&daemonLogPath)
	// fServerURL := cmdServer.Flag("server", "server url").Short('t').String()

	fServer := cmdServer.Flag("server", "frpc token").Short('s').Default("cc.ipviewer.cn:17000").String()
	fToken := cmdServer.Flag("token", "frpc server").Short('t').Default("taikang").String()
	fAuth := cmdServer.Flag("auth", "frpc auth").Short('a').String()

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
	case "frpc":
		subcmd.DoFrpc()
		return
	case "version":
		fmt.Println(version)
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
		fmt.Println(string(data))
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

	if _, err := os.Stat("/sdcard/tmp"); err != nil {
		os.MkdirAll("/sdcard/tmp", 0755)
	}
	os.Setenv("TMPDIR", "/sdcard/tmp")

	if *fDaemon {
		log.Println("run atx-agent in background")

		cntxt := runDaemon()
		if cntxt == nil {
			log.Printf("atx-agent listening on %v", listenAddr)
			return
		}
		defer cntxt.Release()

		log.Println("- - - - - - - - - - - - - - -")
		log.Println("daemon started")
		setupLogrotate()

	}

	log.Printf("atx-agent version %s\n", version)
	lazyInit()
	devInfo := getDeviceInfo()
	// show ip
	outIp, err := getOutboundIP()
	if err == nil {
		fmt.Printf("Device IP: %v\n", outIp)
		if *fServer != "" {
			fmt.Printf("Listen on http://%s.tk.ipviewer.cn", devInfo.Udid)
		}
	} else {
		fmt.Printf("Internet is not connected.")
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatal(err)
	}

	// minicap + minitouch
	width, height := devInfo.Display.Width, devInfo.Display.Height
	service.Add("minicap", cmdctrl.CommandInfo{
		Environ: []string{fmt.Sprintf("LD_LIBRARY_PATH=%v", expath)},
		Args: []string{fmt.Sprintf("%v/%v", expath, "minicap"), "-S", "-P",
			fmt.Sprintf("%dx%d@%dx%d/0", width, height, displayMaxWidthHeight, displayMaxWidthHeight)},
	})

	service.Add("remote_adbd", cmdctrl.CommandInfo{
		MaxRetries: 2,
		Shell:      false,
		OnStart: func() error {
			runShell("stop", "adbd")
			runShell("setprop", "service.adb.tcp.port", "5555")
			runShell("start", "adbd")
			return nil
		},
		ArgsFunc: func() ([]string, error) {
			ex, err := os.Executable()
			if err != nil {
				return []string{}, err
			}
			args := []string{ex, "frpc",
				"-k", "stcp", "-l", "5555",
				"-n", "adbd_" + devInfo.Udid[0:8],
				"--ue", "--uc",
				"-s", *fServer, "-t", *fToken,
				"--role", "server", "--sk", "secadb"}
			return args, nil
		},
	})

	service.Add("remote_http", cmdctrl.CommandInfo{
		MaxRetries: 2,
		Shell:      false,
		ArgsFunc: func() ([]string, error) {
			ex, err := os.Executable()
			if err != nil {
				return []string{}, err
			}
			host := "127.0.0.1"
			port := "7912"
			arr := strings.Split(listenAddr, ":")
			if len(arr) == 2 {
				host = arr[0]
				port = arr[1]
			}
			_ = host
			args := []string{ex, "frpc",
				"-k", "http", "-l", port,
				"-n", devInfo.Udid,
				"--ue", "--uc",
				"-s", *fServer, "-t", *fToken,
				"--sd", devInfo.Udid}
			if *fAuth != "" {
				strs := strings.Split(*fAuth, ":")
				if len(strs) >= 2 {
					args = append(args, []string{
						"--http_user=" + strs[0],
						"--http_pass=" + strs[1],
					}...)
				} else {
					args = append(args, []string{
						"--http_user=" + *fAuth,
						"--http_pass=" + *fAuth,
					}...)
				}
			}
			return args, nil
		},
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
		Args:       []string{fmt.Sprintf("%v/%v", expath, "minitouch")},
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
			"com.github.uiautomator.test/androidx.test.runner.AndroidJUnitRunner"}, // update for android-uiautomator-server.apk>=2.3.2
		//"com.github.uiautomator.test/android.support.test.runner.AndroidJUnitRunner"},
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

	if *fServer != "" {
		if err := service.Start("remote_http"); err != nil {
			log.Println("frpc remote_http start failed:", err)
		}
		log.Printf("you can visit with [%s]", devInfo.Udid)
		if err := service.Start("remote_adbd"); err != nil {
			log.Println("frpc remote_adbd start failed:", err)
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
