package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os/user"
	"path/filepath"

	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DeanThompson/syncmap"
	"github.com/codeskyblue/kexec"
	"github.com/franela/goreq"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/openatx/androidutils"
	"github.com/openatx/atx-agent/cmdctrl"
	"github.com/pkg/errors"
	"github.com/shogo82148/androidbinary/apk"
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
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	devInfo := getDeviceInfo()
	width, height := devInfo.Display.Width, devInfo.Display.Height
	service.Add("minicap", cmdctrl.CommandInfo{
		Environ: []string{"LD_LIBRARY_PATH=/data/local/tmp"},
		Args:    []string{"/data/local/tmp/minicap", "-S", "-P", fmt.Sprintf("%dx%d@800x800/0", width, height)},
	})
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

func runShell(args ...string) (output []byte, err error) {
	return exec.Command("sh", "-c", strings.Join(args, " ")).CombinedOutput()
}

func runShellOutput(args ...string) (output []byte, err error) {
	return exec.Command("sh", "-c", strings.Join(args, " ")).Output()
}

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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var (
	propOnce       sync.Once
	properties     map[string]string
	deviceRotation int
)

func getProperty(name string) string {
	propOnce.Do(func() {
		var err error
		properties, err = androidutils.Properties()
		if err != nil {
			log.Println("getProperty err:", err)
			properties = make(map[string]string)
		}
	})
	return properties[name]
}

func installRequirements() error {
	log.Println("install uiautomator apk")
	if err := installUiautomatorAPK(); err != nil {
		return err
	}
	return installMinicap()
}

const (
	apkVersionCode = 4
	apkVersionName = "1.0.4"
)

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

func installAPK(path string) error {
	// -g: grant all runtime permissions
	// -d: allow version code downgrade
	// -r: replace existing application
	sdk, _ := strconv.Atoi(getProperty("ro.build.version.sdk"))
	cmds := []string{"pm", "install", "-d", "-r", path}
	if sdk >= 23 { // android 6.0
		cmds = []string{"pm", "install", "-d", "-r", "-g", path}
	}
	out, err := runShell(cmds...)
	if err != nil {
		matches := regexp.MustCompile(`Failure \[([\w_ ]+)\]`).FindStringSubmatch(string(out))
		if len(matches) > 0 {
			return errors.Wrap(err, matches[0])
		}
		return errors.Wrap(err, string(out))
	}
	return nil
}

var canFixedInstallFails = map[string]bool{
	"INSTALL_FAILED_PERMISSION_MODEL_DOWNGRADE": true,
	"INSTALL_FAILED_UPDATE_INCOMPATIBLE":        true,
	"INSTALL_FAILED_VERSION_DOWNGRADE":          true,
}

func installAPKForce(path string, packageName string) error {
	err := installAPK(path)
	if err == nil {
		return nil
	}
	errType := regexp.MustCompile(`INSTALL_FAILED_[\w_]+`).FindString(err.Error())
	if !canFixedInstallFails[errType] {
		return err
	}
	runShell("pm", "uninstall", packageName)
	return installAPK(path)
}

func installUiautomatorAPK() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if checkUiautomatorInstalled() {
		return nil
	}
	baseURL := "https://github.com/openatx/android-uiautomator-server/releases/download/" + apkVersionName
	if _, err := httpDownload("/data/local/tmp/app-debug.apk", baseURL+"/app-uiautomator.apk", 0644); err != nil {
		return err
	}
	if _, err := httpDownload("/data/local/tmp/app-debug-test.apk", baseURL+"/app-uiautomator-test.apk", 0644); err != nil {
		return err
	}
	if err := installAPKForce("/data/local/tmp/app-debug.apk", "com.github.uiautomator"); err != nil {
		return err
	}
	if err := installAPKForce("/data/local/tmp/app-debug-test.apk", "com.github.uiautomator.test"); err != nil {
		return err
	}
	return nil
}

func installMinicap() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	log.Println("install minicap")
	if fileExists("/data/local/tmp/minicap") && fileExists("/data/local/tmp/minicap.so") {
		if err := Screenshot("/dev/null"); err != nil {
			log.Println("err:", err)
		} else {
			return nil
		}
	}
	minicapSource := "https://github.com/codeskyblue/stf-binaries/raw/master/node_modules/minicap-prebuilt/prebuilt"
	propOutput, err := runShell("getprop")
	if err != nil {
		return err
	}
	re := regexp.MustCompile(`\[(.*?)\]:\s*\[(.*?)\]`)
	matches := re.FindAllStringSubmatch(string(propOutput), -1)
	props := make(map[string]string)
	for _, m := range matches {
		var key = m[1]
		var val = m[2]
		props[key] = val
	}
	abi := props["ro.product.cpu.abi"]
	sdk := props["ro.build.version.sdk"]
	pre := props["ro.build.version.preview_sdk"]
	if pre != "" && pre != "0" {
		sdk = sdk + pre
	}
	binURL := strings.Join([]string{minicapSource, abi, "bin", "minicap"}, "/")
	_, err = httpDownload("/data/local/tmp/minicap", binURL, 0755)
	if err != nil {
		return err
	}
	libURL := strings.Join([]string{minicapSource, abi, "lib", "android-" + sdk, "minicap.so"}, "/")
	_, err = httpDownload("/data/local/tmp/minicap.so", libURL, 0644)
	if err != nil {
		return err
	}
	return nil
}

func Screenshot(filename string) (err error) {
	output, err := runShellOutput("LD_LIBRARY_PATH=/data/local/tmp", "/data/local/tmp/minicap", "-i")
	if err != nil {
		return
	}
	var f MinicapInfo
	if er := json.Unmarshal([]byte(output), &f); er != nil {
		err = fmt.Errorf("minicap not supported: %v", er)
		return
	}
	if _, err = runShell(
		"LD_LIBRARY_PATH=/data/local/tmp",
		"/data/local/tmp/minicap",
		"-P", fmt.Sprintf("%dx%d@%dx%d/%d", f.Width, f.Height, f.Width, f.Height, f.Rotation),
		"-s", ">"+filename); err != nil {
		return
	}
	return nil
}

type DownloadManager struct {
	db map[string]*DownloadProxy
	mu sync.Mutex
	n  int
}

func newDownloadManager() *DownloadManager {
	return &DownloadManager{
		db: make(map[string]*DownloadProxy, 10),
	}
}

func (m *DownloadManager) Get(id string) *DownloadProxy {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.db[id]
}

func (m *DownloadManager) Put(di *DownloadProxy) (id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n += 1
	id = strconv.Itoa(m.n)
	m.db[id] = di
	di.Id = id
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

type DownloadProxy struct {
	writer     io.Writer
	Id         string      `json:"id"`
	TotalSize  int         `json:"totalSize"`
	CopiedSize int         `json:"copiedSize"`
	Message    string      `json:"message"`
	Error      string      `json:"error,omitempty"`
	ExtraData  interface{} `json:"extraData,omitempty"`
	wg         sync.WaitGroup
}

func newDownloadProxy(wr io.Writer, totalSize int) *DownloadProxy {
	di := &DownloadProxy{
		writer:    wr,
		TotalSize: totalSize,
	}
	di.wg.Add(1)
	return di
}

func (d *DownloadProxy) Write(data []byte) (int, error) {
	n, err := d.writer.Write(data)
	d.CopiedSize += n
	return n, err
}

// Should only call once
func (d *DownloadProxy) Done() {
	d.wg.Done()
}

func (d *DownloadProxy) Wait() {
	d.wg.Wait()
}

func AsyncDownloadTo(url string, filepath string, autoRelease bool) (di *DownloadProxy, err error) {
	// do real http download
	res, err := goreq.Request{
		Uri:             url,
		MaxRedirects:    10,
		RedirectHeaders: true,
	}.Do()
	if err != nil {
		return
	}
	if res.StatusCode != http.StatusOK {
		body, err := res.Body.ToString()
		res.Body.Close()
		if err != nil && err != bytes.ErrTooLarge {
			return nil, fmt.Errorf("Expected HTTP Status code: %d", res.StatusCode)
		}
		return nil, errors.New(body)
	}
	file, err := os.Create(filepath)
	if err != nil {
		res.Body.Close()
		return
	}
	var totalSize int
	fmt.Sscanf(res.Header.Get("Content-Length"), "%d", &totalSize)
	di = newDownloadProxy(file, totalSize)
	downloadKey := downManager.Put(di)
	go func() {
		if autoRelease {
			defer downManager.Del(downloadKey)
		}
		defer di.Done()
		defer res.Body.Close()
		defer file.Close()
		io.Copy(di, res.Body)
	}()
	return
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
	template.Must(template.New(filename).Parse(string(content))).Execute(w, nil)
}

var (
	ErrJpegWrongFormat = errors.New("jpeg format error, not starts with 0xff,0xd8")
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
func translateMinicap(conn net.Conn, jpgC chan []byte, quitC chan bool) error {
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
		case <-quitC:
			return nil
		default:
			// TODO(ssx): image should not wait or it will stuck here
		}
	}
	return err
}

// var workers = syncmap.New()

// goInstallApk := func(filepath string) (key string){

// 		}

func ServeHTTP(lis net.Listener, tunnel *TunnelProxy) error {
	m := mux.NewRouter()

	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		renderHTML(w, "index.html")
	})

	m.HandleFunc("/remote", func(w http.ResponseWriter, r *http.Request) {
		renderHTML(w, "remote.html")
	})

	m.HandleFunc("/shell", func(w http.ResponseWriter, r *http.Request) {
		command := r.FormValue("command")
		if command == "" {
			command = r.FormValue("c")
		}
		output, err := exec.Command("sh", "-c", command).CombinedOutput()
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"output": string(output),
			"error":  err,
		})
	}).Methods("GET", "POST")

	m.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		log.Println("stop all service")
		service.StopAll()
		log.Println("service stopped")
		io.WriteString(w, "Finished!")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel() // The document says need to call cancel(), but I donot known why.
			httpServer.Shutdown(ctx)
		}()
	})

	m.HandleFunc("/uiautomator", func(w http.ResponseWriter, r *http.Request) {
		err := service.Start("uiautomator")
		if err == nil {
			io.WriteString(w, "Success")
		} else {
			http.Error(w, err.Error(), 500)
		}
	}).Methods("POST")

	m.HandleFunc("/uiautomator", func(w http.ResponseWriter, r *http.Request) {
		err := service.Stop("uiautomator")
		if err == nil {
			io.WriteString(w, "Success")
		} else {
			http.Error(w, err.Error(), 500)
		}
	}).Methods("DELETE")

	m.HandleFunc("/raw/{filepath:.*}", func(w http.ResponseWriter, r *http.Request) {
		filepath := mux.Vars(r)["filepath"]
		http.ServeFile(w, r, filepath)
	})

	m.HandleFunc("/info/battery", func(w http.ResponseWriter, r *http.Request) {
		devInfo := getDeviceInfo()
		devInfo.Battery.Update()
		if err := tunnel.UpdateInfo(devInfo); err != nil {
			// log.Printf("update info err: %v", err)
			io.WriteString(w, "Failure "+err.Error())
			return
		}
		io.WriteString(w, "Success")
	}).Methods("POST")

	m.HandleFunc("/info/rotation", func(w http.ResponseWriter, r *http.Request) {
		var direction int // 0,1,2,3
		json.NewDecoder(r.Body).Decode(&direction)
		deviceRotation = direction * 90
		log.Println("rotation change received:", deviceRotation)
		devInfo := getDeviceInfo()
		width, height := devInfo.Display.Width, devInfo.Display.Height
		go service.UpdateArgs("minicap", "/data/local/tmp/minicap", "-S", "-P", fmt.Sprintf("%dx%d@800x800/%d", width, height, deviceRotation))
		io.WriteString(w, "Success")
	})

	m.HandleFunc("/upload/{target:.*}", func(w http.ResponseWriter, r *http.Request) {
		target := mux.Vars(r)["target"]
		if runtime.GOOS != "windows" {
			target = "/" + target
		}
		var fileMode os.FileMode
		if _, err := fmt.Sscanf(r.FormValue("mode"), "%o", &fileMode); err != nil {
			log.Printf("invalid file mode: %s", r.FormValue("mode"))
			fileMode = 0644
		} // %o base 8

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer func() {
			file.Close()
			r.MultipartForm.RemoveAll()
		}()
		if strings.HasSuffix(target, "/") {
			target = path.Join(target, header.Filename)
		}

		targetDir := filepath.Dir(target)
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			os.MkdirAll(targetDir, 0755)
		}

		fd, err := os.Create(target)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer fd.Close()
		written, err := io.Copy(fd, file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if fileMode != 0 {
			os.Chmod(target, fileMode)
		}
		if fileInfo, err := os.Stat(target); err == nil {
			fileMode = fileInfo.Mode()
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"target": target,
			"size":   written,
			"mode":   fmt.Sprintf("0%o", fileMode),
		})
	})

	installThreads := syncmap.New()

	m.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		dst := r.FormValue("filepath")
		url := r.FormValue("url")
		var fileMode os.FileMode
		if _, err := fmt.Sscanf(r.FormValue("mode"), "%o", &fileMode); err != nil {
			log.Printf("invalid file mode: %s", r.FormValue("mode"))
			fileMode = 0644
		} // %o base 8
		key := background.HTTPDownload(url, dst, fileMode)
		io.WriteString(w, key)
	}).Methods("POST")

	m.HandleFunc("/download/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := mux.Vars(r)["key"]
		status := background.Get(key)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}).Methods("GET")

	// TODO: need test
	m.HandleFunc("/install-local", func(w http.ResponseWriter, r *http.Request) {
		filepath := r.FormValue("filepath")
		key := fmt.Sprintf("k%d", time.Now().Nanosecond())
		go func(key string) {
			update := func(v map[string]string) {
				installThreads.Set(key, v)
			}
			go func() {
				time.Sleep(5 * time.Minute)
				installThreads.Delete(key)
			}()
			update(map[string]string{"message": "apk parsing"})
			pkg, er := apk.OpenFile(filepath)
			if er != nil {
				update(map[string]string{
					"error":   er.Error(),
					"message": "androidbinary parse apk error",
				})
				return
			}
			defer pkg.Close()
			packageName := pkg.PackageName()
			update(map[string]string{
				"message":   "installing",
				"extraData": packageName,
			})
			// install apk
			err := installAPKForce(filepath, packageName)
			if err != nil {
				update(map[string]string{
					"error":   err.Error(),
					"message": "error install",
				})
			} else {
				update(map[string]string{"message": "success installed"})
			}
		}(key)
		io.WriteString(w, key)
	})

	m.HandleFunc("/install", func(w http.ResponseWriter, r *http.Request) {
		url := r.FormValue("url")
		filepath := r.FormValue("filepath")
		if filepath == "" {
			filepath = "/sdcard/tmp.apk"
		}
		di, err := AsyncDownloadTo(url, filepath, false) // use false to disable DownloadProxy auto recycle
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		di.Message = "downloading"
		go func() {
			di.Wait() // wait download finished
			if runtime.GOOS == "windows" {
				log.Println("fake pm install")
				downManager.Del(di.Id)
				return
			}
			defer downManager.DelayDel(di.Id, time.Minute*5)

			di.Message = "apk parsing"
			pkg, er := apk.OpenFile(filepath)
			if er != nil {
				di.Error = er.Error()
				di.Message = "androidbinary parse apk error"
				return
			}
			defer pkg.Close()
			packageName := pkg.PackageName()
			di.ExtraData = packageName
			// install apk
			di.Message = "installing"
			err := installAPKForce(filepath, packageName)
			if err != nil {
				di.Error = err.Error()
				di.Message = "error install"
			} else {
				di.Message = "success installed"
			}
		}()
		io.WriteString(w, di.Id)
	}).Methods("POST")

	m.HandleFunc("/install/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		dp := downManager.Get(id)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dp)
	}).Methods("GET")

	m.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, version)
	})

	m.HandleFunc("/minicap", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Print("upgrade:", err)
			return
		}
		defer ws.Close()

		const wsWriteWait = 10 * time.Second
		wsWrite := func(messageType int, data []byte) error {
			ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
			return ws.WriteMessage(messageType, data)
		}
		wsWrite(websocket.TextMessage, []byte("start @minicap service"))
		if err := service.Start("minicap"); err != nil && err != cmdctrl.ErrAlreadyRunning {
			wsWrite(websocket.TextMessage, []byte("@minicap service start failed: "+err.Error()))
			return
		}
		// TODO
		wsWrite(websocket.TextMessage, []byte("dial unix:@minicap"))
		log.Printf("minicap connection: %v", r.RemoteAddr)
		jpgC := make(chan []byte, 10)
		quitC := make(chan bool, 2)

		go func() {
			defer close(jpgC)
			retries := 0
			for {
				if retries > 10 {
					wsWrite(websocket.TextMessage, []byte("@minicap listen timeout, possibly minicap not installed"))
					break
				}
				conn, err := net.Dial("unix", "@minicap")
				if err != nil {
					retries++
					log.Printf("dial @minicap err: %v, wait 0.5s", err)
					select {
					case <-quitC:
						return
					case <-time.After(500 * time.Millisecond):
					}
					continue
				}
				retries = 0 // connected, reset retries
				if er := translateMinicap(conn, jpgC, quitC); er == nil {
					conn.Close()
					log.Println("transfer closed")
					break
				} else {
					conn.Close()
					log.Println("minicap read error, try to read again")
				}
			}
		}()
		go func() {
			for {
				if _, _, err := ws.ReadMessage(); err != nil {
					quitC <- true
					break
				}
			}
		}()
		for data := range jpgC {
			if err := wsWrite(websocket.TextMessage, []byte("data size: "+strconv.Itoa(len(data)))); err != nil {
				break
			}
			if err := wsWrite(websocket.BinaryMessage, data); err != nil {
				break
			}
		}
		quitC <- true
		log.Println("stream finished")
	})

	// FIXME(ssx): screenrecord is not good enough, need to change later
	var recordCmd *exec.Cmd
	var recordDone = make(chan bool, 1)
	var recordLock sync.Mutex
	var recordFolder = "/sdcard/screenrecords/"
	var recordRunning = false

	m.HandleFunc("/screenrecord", func(w http.ResponseWriter, r *http.Request) {
		recordLock.Lock()
		defer recordLock.Unlock()

		if recordCmd != nil {
			http.Error(w, "screenrecord not closed", 400)
			return
		}
		os.RemoveAll(recordFolder)
		os.MkdirAll(recordFolder, 0755)
		recordCmd = exec.Command("screenrecord", recordFolder+"0.mp4")
		if err := recordCmd.Start(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		recordRunning = true
		go func() {
			for i := 1; recordCmd.Wait() == nil && i <= 20 && recordRunning; i++ { // set limit, to prevent too many videos. max 1 hour
				recordCmd = exec.Command("screenrecord", recordFolder+strconv.Itoa(i)+".mp4")
				if err := recordCmd.Start(); err != nil {
					log.Println("screenrecord error:", err)
					break
				}
			}
			recordDone <- true
		}()
		io.WriteString(w, "screenrecord started")
	}).Methods("POST")

	m.HandleFunc("/screenrecord", func(w http.ResponseWriter, r *http.Request) {
		recordLock.Lock()
		defer recordLock.Unlock()

		recordRunning = false
		if recordCmd != nil {
			if recordCmd.Process != nil {
				recordCmd.Process.Signal(os.Interrupt)
			}
			select {
			case <-recordDone:
			case <-time.After(5 * time.Second):
				// force kill
				exec.Command("pkill", "screenrecord").Run()
			}
			recordCmd = nil
		}
		w.Header().Set("Content-Type", "application/json")
		files, _ := ioutil.ReadDir(recordFolder)
		videos := []string{}
		for i := 0; i < len(files); i++ {
			videos = append(videos, fmt.Sprintf(recordFolder+"%d.mp4", i))
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"videos": videos,
		})
	}).Methods("PUT")

	m.HandleFunc("/upgrade", func(w http.ResponseWriter, r *http.Request) {
		ver := r.FormValue("version")
		var err error
		if ver == "" {
			ver, err = getLatestVersion()
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		if ver == version {
			io.WriteString(w, "current version is already "+version)
			return
		}
		err = doUpdate(ver)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		io.WriteString(w, "update finished, restarting")
		go func() {
			log.Printf("restarting server")
			runDaemon()
		}()
	})

	m.HandleFunc("/term", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			handleTerminalWebsocket(w, r)
			return
		}
		renderHTML(w, "terminal.html")
	})

	m.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		info := getDeviceInfo()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})

	screenshotFilename := "/data/local/tmp/minicap-screenshot.jpg"
	if username := currentUserName(); username != "" {
		screenshotFilename = "/data/local/tmp/minicap-screenshot-" + username + ".jpg"
	}

	m.HandleFunc("/screenshot", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/screenshot/0", 302)
	}).Methods("GET")

	target, _ := url.Parse("http://127.0.0.1:9008")
	uiautomatorProxy := httputil.NewSingleHostReverseProxy(target)
	m.Handle("/jsonrpc/0", uiautomatorProxy)
	m.Handle("/ping", uiautomatorProxy)
	m.HandleFunc("/screenshot/0", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("minicap") == "false" || strings.ToLower(getProperty("ro.product.manufacturer")) == "meizu" {
			uiautomatorProxy.ServeHTTP(w, r)
			return
		}
		if err := Screenshot(screenshotFilename); err != nil {
			log.Printf("screenshot[minicap] error: %v", err)
			uiautomatorProxy.ServeHTTP(w, r)
		} else {
			w.Header().Set("X-Screenshot-Method", "minicap")
			http.ServeFile(w, r, screenshotFilename)
		}
	})

	m.Handle("/assets/{(.*)}", http.StripPrefix("/assets", http.FileServer(Assets)))

	httpServer = &http.Server{Handler: m} // url(/stop) need it.
	return httpServer.Serve(lis)
}

func runDaemon() {
	environ := os.Environ()
	// env:IGNORE_SIGHUP forward stdout and stderr to file
	// env:ATX_AGENT will ignore -d flag
	environ = append(environ, "IGNORE_SIGHUP=true", "ATX_AGENT=1")
	cmd := kexec.Command(os.Args[0], os.Args[1:]...)
	cmd.Env = environ
	cmd.Start()
	select {
	case err := <-GoFunc(cmd.Wait):
		log.Fatalf("server started failed, %v", err)
	case <-time.After(200 * time.Millisecond):
		fmt.Printf("server started, listening on %v:%d\n", mustGetOoutboundIP(), listenPort)
	}
}

func main() {
	fDaemon := flag.Bool("d", false, "run daemon")
	flag.IntVar(&listenPort, "p", 7912, "listen port") // Create on 2017/09/12
	fVersion := flag.Bool("v", false, "show version")
	fRequirements := flag.Bool("r", false, "install minicap and uiautomator.apk")
	fStop := flag.Bool("stop", false, "stop server")
	fTunnelServer := flag.String("t", "", "tunnel server address")
	fNoUiautomator := flag.Bool("nouia", false, "not start uiautomator")
	flag.Parse()

	if *fVersion {
		fmt.Println(version)
		return
	}

	if *fStop {
		_, err := http.Get("http://127.0.0.1:7912/stop")
		if err != nil {
			log.Println(err)
		} else {
			log.Println("server stopped")
		}
		return
	}

	if *fRequirements {
		log.Println("check dependencies")
		if err := installRequirements(); err != nil {
			// panic(err)
			log.Println("requirements not ready:", err)
			return
		}
	}

	if *fDaemon && os.Getenv("ATX_AGENT") == "" {
		runDaemon()
		return
	}

	if os.Getenv("IGNORE_SIGHUP") == "true" {
		fmt.Println("Enter into daemon mode")
		f, err := os.Create("/sdcard/atx-agent.log")
		if err != nil {
			panic(err)
		}
		defer f.Close()

		os.Stdout = f
		os.Stderr = f
		os.Stdin = nil

		log.SetOutput(f)
		log.Println("Ignore SIGHUP")
		signal.Ignore(syscall.SIGHUP)

		// kill previous daemon first
		log.Println("Kill server")
		_, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/stop", listenPort))
		if err == nil {
			log.Println("wait previous server stopped")
			time.Sleep(1000 * time.Millisecond) // server will quit in 0.1s
		} else {
			log.Println(err)
		}
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

	// uiautomator
	service.Add("uiautomator", cmdctrl.CommandInfo{
		Args: []string{"am", "instrument", "-w", "-r",
			"-e", "debug", "false",
			"-e", "class", "com.github.uiautomator.stub.Stub",
			"com.github.uiautomator.test/android.support.test.runner.AndroidJUnitRunner"},
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		MaxRetries:      3,
		RecoverDuration: 30 * time.Second,
	})
	if !*fNoUiautomator {
		if _, err := runShell("am", "start", "-W", "-n", "com.github.uiautomator/.MainActivity"); err != nil {
			log.Println("start uiautomator err:", err)
		}
		if err := service.Start("uiautomator"); err != nil {
			log.Println("uiautomator start failed:", err)
		}
	}

	tunnel := &TunnelProxy{ServerAddr: *fTunnelServer}
	if *fTunnelServer != "" {
		go tunnel.RunForever()
	}
	// run server forever
	if err := ServeHTTP(listener, tunnel); err != nil {
		log.Println("server quit:", err)
	}
}
