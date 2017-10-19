package main

import (
	"encoding/json"

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

	"github.com/codeskyblue/kexec"
	"github.com/franela/goreq"
	"github.com/gorilla/mux"
	"github.com/openatx/androidutils"
	"github.com/pkg/errors"
	"github.com/shogo82148/androidbinary/apk"
)

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
	propOnce   sync.Once
	properties map[string]string
)

func getProperty(name string) string {
	propOnce.Do(func() {
		properties = make(map[string]string)
		propOutput, err := runShell("getprop")
		if err != nil {
			panic(err)
		}
		re := regexp.MustCompile(`\[(.*?)\]:\s*\[(.*?)\]`)
		matches := re.FindAllStringSubmatch(string(propOutput), -1)
		for _, m := range matches {
			var key = m[1]
			var val = m[2]
			properties[key] = val
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
	out, err := runShell("pm", "install", "-d", "-r", path)
	if err != nil {
		matches := regexp.MustCompile(`Failure \[([\w_]+)\]`).FindStringSubmatch(string(out))
		if len(matches) > 0 {
			return errors.Wrap(err, matches[0])
		}
		return err
	}
	return nil
}

func installAPKForce(path string) error {
	err := installAPK(path)
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "INSTALL_FAILED_UPDATE_INCOMPATIBLE") {
		return err
	}
	pkg, er := apk.OpenFile(path)
	if er != nil {
		return errors.Wrap(err, er.Error())
	}
	defer pkg.Close()
	runShell("pm", "uninstall", pkg.PackageName())
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
	if err := installAPKForce("/data/local/tmp/app-debug.apk"); err != nil {
		return err
	}
	if err := installAPKForce("/data/local/tmp/app-debug-test.apk"); err != nil {
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
	Id         string `json:"id"`
	TotalSize  int    `json:"totalSize"`
	CopiedSize int    `json:"copiedSize"`
	Message    string `json:"message"`
	Error      string `json:"error,omitempty"`
	wg         sync.WaitGroup
}

func newDownloadProxy(wr io.Writer) *DownloadProxy {
	di := &DownloadProxy{
		writer: wr,
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

var downManager = newDownloadManager()

func AsyncDownloadTo(url string, filepath string, autoRelease bool) (di *DownloadProxy, err error) {
	res, err := goreq.Request{
		Uri:             url,
		MaxRedirects:    10,
		RedirectHeaders: true,
	}.Do()
	if err != nil {
		return
	}
	file, err := os.Create(filepath)
	if err != nil {
		res.Body.Close()
		return
	}
	di = newDownloadProxy(file)
	fmt.Sscanf(res.Header.Get("Content-Length"), "%d", &di.TotalSize)
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

func ServeHTTP(port int) error {
	m := mux.NewRouter()

	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		file, err := Assets.Open("index.html")
		if err != nil {
			http.Error(w, "404 page not found", 404)
			return
		}
		content, _ := ioutil.ReadAll(file)
		template.Must(template.New("index").Parse(string(content))).Execute(w, nil)
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
	})

	m.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "Finished!")
		go httpServer.Shutdown(nil)
	})

	m.HandleFunc("/uiautomator", func(w http.ResponseWriter, r *http.Request) {
		err := safeRunUiautomator()
		if err == nil {
			io.WriteString(w, "Success")
		} else {
			io.WriteString(w, err.Error())
		}
	}).Methods("POST")

	m.HandleFunc("/screenshot", func(w http.ResponseWriter, r *http.Request) {
		if strings.ToLower(getProperty("ro.product.manufacturer")) == "meizu" {
			http.Redirect(w, r, "/screenshot/0", 302)
			return
		}
		imagePath := "/data/local/tmp/minicap-screenshot.jpg"
		if err := Screenshot(imagePath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.ServeFile(w, r, imagePath)
	}).Methods("GET")

	m.HandleFunc("/upload/{filepath:.*}", func(w http.ResponseWriter, r *http.Request) {
		filepath := mux.Vars(r)["filepath"]
		if runtime.GOOS != "windows" {
			filepath = "/" + filepath
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
		if strings.HasSuffix(filepath, "/") {
			filepath = path.Join(filepath, header.Filename)
		}
		target, err := os.Create(filepath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer target.Close()
		written, err := io.Copy(target, file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if fileMode != 0 {
			os.Chmod(filepath, fileMode)
		}
		if fileInfo, err := os.Stat(filepath); err == nil {
			fileMode = fileInfo.Mode()
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"target": filepath,
			"size":   written,
			"mode":   fmt.Sprintf("0%o", fileMode),
		})
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

			// -g: grant all runtime permissions
			// -d: allow version code downgrade
			// -r: replace existing application
			di.Message = "installing"
			sdk, _ := strconv.Atoi(getProperty("ro.build.version.sdk"))
			cmds := []string{"pm", "install", "-d", "-r", filepath}
			if sdk >= 23 { // android 6.0
				cmds = []string{"pm", "install", "-d", "-r", "-g", filepath}
			}
			output, err := runShell(cmds...)
			if err != nil {
				di.Error = err.Error()
				di.Message = string(output)
			} else {
				di.Message = "success installed"
			}
			downManager.DelayDel(di.Id, time.Minute*5)
		}()
		io.WriteString(w, di.Id)
	}).Methods("POST")

	m.HandleFunc("/install/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		dp := downManager.Get(id)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dp)
	})

	m.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, version)
	})

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

	m.HandleFunc("/term", handleTerminalWebsocket)

	target, _ := url.Parse("http://127.0.0.1:9008")
	uiautomatorProxy := httputil.NewSingleHostReverseProxy(target)
	http.Handle("/jsonrpc/0", uiautomatorProxy)
	http.Handle("/ping", uiautomatorProxy)
	http.HandleFunc("/screenshot/0", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("minicap") == "false" || strings.ToLower(getProperty("ro.product.manufacturer")) == "meizu" {
			uiautomatorProxy.ServeHTTP(w, r)
			return
		}
		imagePath := "/data/local/tmp/minicap-screenshot.jpg"
		if err := Screenshot(imagePath); err != nil {
			log.Printf("screenshot[minicap] error: %v", err)
			uiautomatorProxy.ServeHTTP(w, r)
		} else {
			http.ServeFile(w, r, imagePath)
		}
	})
	http.Handle("/assets/", http.FileServer(Assets))
	http.Handle("/", m)
	httpServer = &http.Server{
		Addr: ":" + strconv.Itoa(port),
	}
	return httpServer.ListenAndServe()
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
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

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
		log.Println("Ignore SIGUP")
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

	// show ip
	outIp, err := getOutboundIP()
	if err == nil {
		fmt.Printf("Listen on http://%v:%d\n", outIp, listenPort)
	} else {
		fmt.Printf("Internet is not connected.")
	}

	go safeRunUiautomator()
	if *fTunnelServer != "" {
		go runTunnelProxy(*fTunnelServer)
	}
	if err := ServeHTTP(listenPort); err != nil {
		log.Println("server quit:", err)
	}
}
