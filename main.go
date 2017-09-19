package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/codeskyblue/kexec"
	"github.com/franela/goreq"
	"github.com/miekg/dns"
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
		panic(err)
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
	return io.Copy(file, resp.Body)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func InstallRequirements() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if fileExists("/data/local/tmp/minicap") && fileExists("/data/local/tmp/minicap.so") && Screenshot("/dev/null") == nil {
		return nil
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
	output, err := runShell("LD_LIBRARY_PATH=/data/local/tmp", "/data/local/tmp/minicap", "-i")
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

func safeRunUiautomator() {
	runUiautomator()
}

func runUiautomator() error {
	c := exec.Command("am", "instrument", "-w", "-r",
		"-e", "debug", "false",
		"-e", "class", "com.github.uiautomator.stub.Stub",
		"com.github.uiautomator.test/android.support.test.runner.AndroidJUnitRunner")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

type DownloadManager struct {
	db map[string]*DownloadProxy
	mu sync.Mutex
	n  int
}

func NewDownloadManager() *DownloadManager {
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
	TotalSize  int    `json:"titalSize"`
	CopiedSize int    `json:"copiedSize"`
	Error      string `json:"error,omitempty"`
	wg         sync.WaitGroup
}

func NewDownloadProxy(wr io.Writer) *DownloadProxy {
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

var downManager = NewDownloadManager()

func AsyncDownloadInto(url string, filepath string, autoRelease bool) (di *DownloadProxy, err error) {
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
	di = NewDownloadProxy(file)
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
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "Hello World!")
	})

	http.HandleFunc("/shell", func(w http.ResponseWriter, r *http.Request) {
		command := r.FormValue("command")
		if command == "" {
			command = r.FormValue("c")
		}
		output, err := exec.Command("sh", "-c", command).CombinedOutput()
		log.Println(err)
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"output": string(output),
		})
	})

	http.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "Finished!")
		go func() {
			time.Sleep(100 * time.Millisecond)
			os.Exit(0)
		}()
	})

	http.HandleFunc("/screenshot", func(w http.ResponseWriter, r *http.Request) {
		imagePath := "/data/local/tmp/minicap-screenshot.jpg"
		if err := Screenshot(imagePath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.ServeFile(w, r, imagePath)
	})

	http.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		url := r.FormValue("url")
		filepath := r.FormValue("filepath")
		res, err := goreq.Request{
			Uri:             url,
			MaxRedirects:    10,
			RedirectHeaders: true,
		}.Do()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		file, err := os.Create(filepath)
		if err != nil {
			res.Body.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		di := NewDownloadProxy(file)
		fmt.Sscanf(res.Header.Get("Content-Length"), "%d", &di.TotalSize)
		downloadKey := downManager.Put(di)
		go func() {
			defer downManager.Del(downloadKey)
			defer res.Body.Close()
			defer file.Close()
			io.Copy(di, res.Body)
		}()
		io.WriteString(w, downloadKey)
	})

	http.HandleFunc("/uploadStats", func(w http.ResponseWriter, r *http.Request) {
		key := r.FormValue("key")
		di := downManager.Get(key)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(di)
	})

	http.HandleFunc("/install", func(w http.ResponseWriter, r *http.Request) {
		url := r.FormValue("url")
		filepath := r.FormValue("filepath")
		if filepath == "" {
			filepath = "/sdcard/tmp.apk"
		}
		di, err := AsyncDownloadInto(url, filepath, false) // use false to disable DownloadProxy auto recycle
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go func() {
			di.Wait() // wait download finished
			if runtime.GOOS == "windows" {
				log.Println("fake am install")
				downManager.Del(di.Id)
				return
			}
			// -g: grant all runtime permissions
			output, err := runShell("pm", "install", "-r", "-g", filepath)
			if err != nil {
				di.Error = err.Error() + " >> " + string(output)
				downManager.DelayDel(di.Id, time.Minute*10)
			} else {
				downManager.Del(di.Id)
			}
		}()
		io.WriteString(w, di.Id)
	})

	return http.ListenAndServe(":"+strconv.Itoa(port), nil)
}

func dnsLookupHost(hostname string) (ip net.IP, err error) {
	if !strings.HasSuffix(hostname, ".") {
		hostname += "."
	}
	m1 := new(dns.Msg)
	m1.Id = dns.Id()
	m1.RecursionDesired = true
	m1.Question = []dns.Question{
		{hostname, dns.TypeA, dns.ClassINET},
	}
	c := new(dns.Client)
	c.SingleInflight = true
	in, _, err := c.Exchange(m1, "8.8.8.8:53")
	if err != nil {
		return nil, err
	}
	if len(in.Answer) == 0 {
		return nil, errors.New("dns return empty answer")
	}
	log.Println(in.Answer[0])
	if t, ok := in.Answer[0].(*dns.A); ok {
		return t.A, nil
	}
	if t, ok := in.Answer[0].(*dns.CNAME); ok {
		return dnsLookupHost(t.Target)
	}
	return nil, errors.New("dns resolve failed: " + hostname)
}

func initialHTTPTransport() {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		DualStack: true,
	}
	// manualy dns resolve
	newDialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, _ := net.SplitHostPort(addr)
		if net.ParseIP(host) == nil {
			ip, err := dnsLookupHost(host)
			if err != nil {
				return nil, err
			}
			addr = ip.String() + ":" + port
		}
		return dialer.DialContext(ctx, network, addr)
	}
	http.DefaultTransport.(*http.Transport).DialContext = newDialContext
	goreq.DefaultTransport.(*http.Transport).DialContext = newDialContext
}

type WriterCounter struct {
	wr         io.Writer
	totalCount int
}

func NewWriterCounter(wr io.Writer) *WriterCounter {
	return &WriterCounter{wr: wr}
}

func (w *WriterCounter) Write(data []byte) (int, error) {
	n, err := w.Write(data)
	w.totalCount += n
	return n, err
}

func main() {
	daemon := flag.Bool("d", false, "run daemon")
	port := flag.Int("p", 7912, "listen port") // Create on 2017/09/12
	flag.Parse()

	initialHTTPTransport()

	log.Println("Check environment")
	if err := InstallRequirements(); err != nil {
		panic(err)
	}

	if *daemon {
		environ := os.Environ()
		environ = append(environ, "IGNORE_SIGHUP=true")
		cmd := kexec.Command(os.Args[0])
		cmd.Env = environ
		cmd.Start()
		select {
		case err := <-GoFunc(cmd.Wait):
			log.Fatalf("server started failed, %v", err)
		case <-time.After(200 * time.Millisecond):
			fmt.Printf("server started, listening on %v:%d\n", mustGetOoutboundIP(), *port)
		}
		return
	}

	if os.Getenv("IGNORE_SIGHUP") == "true" {
		fmt.Println("Enter into daemon mode")
		f, err := os.Create("/sdcard/atx-agent.log")
		if err != nil {
			panic(err)
		}
		defer f.Close()
		log.SetOutput(f)
		log.Println("Ignore SIGUP")
		signal.Ignore(syscall.SIGHUP)
	} else {
		outIp, err := getOutboundIP()
		if err == nil {
			fmt.Printf("IP: %v\n", outIp)
		} else {
			fmt.Printf("Internet is not connected.")
		}
	}

	go safeRunUiautomator()
	log.Fatal(ServeHTTP(*port))
}
