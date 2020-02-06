package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/openatx/atx-agent/jsonrpc"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/mholt/archiver"
	"github.com/openatx/androidutils"
	"github.com/openatx/atx-agent/cmdctrl"
	"github.com/prometheus/procfs"
	"github.com/rs/cors"
)

type Server struct {
	// tunnel     *TunnelProxy
	httpServer *http.Server
}

func NewServer() *Server {
	server := &Server{}
	server.initHTTPServer()
	return server
}

func (server *Server) initHTTPServer() {
	m := mux.NewRouter()

	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		renderHTML(w, "index.html")
	})

	m.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, version)
	})

	m.HandleFunc("/remote", func(w http.ResponseWriter, r *http.Request) {
		renderHTML(w, "remote.html")
	})

	// jsonrpc client to call uiautomator
	rpcc := jsonrpc.NewClient("http://127.0.0.1:9008/jsonrpc/0")
	rpcc.ErrorCallback = func() error {
		service.Restart("uiautomator")
		// if !service.Running("uiautomator") {
		// 	service.Start("uiautomator")
		// }
		return nil
	}
	rpcc.ErrorFixTimeout = 40 * time.Second
	rpcc.ServerOK = func() bool {
		return service.Running("uiautomator")
	}

	m.HandleFunc("/newCommandTimeout", func(w http.ResponseWriter, r *http.Request) {
		var timeout int
		err := json.NewDecoder(r.Body).Decode(&timeout) // TODO: auto get rotation
		if err != nil {
			http.Error(w, "Empty payload", 400) // bad request
			return
		}
		cmdTimeout := time.Duration(timeout) * time.Second
		uiautomatorTimer.Reset(cmdTimeout)
		renderJSON(w, map[string]interface{}{
			"success":     true,
			"description": fmt.Sprintf("newCommandTimeout updated to %v", cmdTimeout),
		})
	}).Methods("POST")

	// robust communicate with uiautomator
	// If the service is down, restart it and wait it recover
	m.HandleFunc("/dump/hierarchy", func(w http.ResponseWriter, r *http.Request) {
		if !service.Running("uiautomator") {
			xmlContent, err := dumpHierarchy()
			if err != nil {
				log.Println("Err:", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			renderJSON(w, map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  xmlContent,
			})
			return
		}
		resp, err := rpcc.RobustCall("dumpWindowHierarchy", false) // false: no compress
		if err != nil {
			log.Println("Err:", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		renderJSON(w, resp)
	})

	m.HandleFunc("/proc/list", func(w http.ResponseWriter, r *http.Request) {
		ps, err := listAllProcs()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		renderJSON(w, ps)
	})

	m.HandleFunc("/proc/{pkgname}/meminfo", func(w http.ResponseWriter, r *http.Request) {
		pkgname := mux.Vars(r)["pkgname"]
		info, err := parseMemoryInfo(pkgname)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		renderJSON(w, info)
	})

	m.HandleFunc("/proc/{pkgname}/meminfo/all", func(w http.ResponseWriter, r *http.Request) {
		pkgname := mux.Vars(r)["pkgname"]
		ps, err := listAllProcs()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		mems := make(map[string]map[string]int, 0)
		for _, p := range ps {
			if len(p.Cmdline) != 1 {
				continue
			}
			if p.Name == pkgname || strings.HasPrefix(p.Name, pkgname+":") {
				info, err := parseMemoryInfo(p.Name)
				if err != nil {
					continue
				}
				mems[p.Name] = info
			}
		}
		renderJSON(w, mems)
	})

	// make(map[int][]int)
	m.HandleFunc("/proc/{pkgname}/cpuinfo", func(w http.ResponseWriter, r *http.Request) {
		pkgname := mux.Vars(r)["pkgname"]
		pid, err := pidOf(pkgname)
		if err != nil {
			http.Error(w, err.Error(), http.StatusGone)
			return
		}
		info, err := readCPUInfo(pid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		renderJSON(w, info)
	})

	m.HandleFunc("/webviews", func(w http.ResponseWriter, r *http.Request) {
		netUnix, err := procfs.NewNetUnix()
		if err != nil {
			return
		}

		unixPaths := make(map[string]bool, 0)
		for _, row := range netUnix.Rows {
			if !strings.HasPrefix(row.Path, "@") {
				continue
			}
			if !strings.Contains(row.Path, "devtools_remote") {
				continue
			}
			unixPaths[row.Path[1:]] = true
		}
		socketPaths := make([]string, 0, len(unixPaths))
		for key := range unixPaths {
			socketPaths = append(socketPaths, key)
		}
		renderJSON(w, socketPaths)
	})

	m.HandleFunc("/webviews/{pkgname}", func(w http.ResponseWriter, r *http.Request) {
		packageName := mux.Vars(r)["pkgname"]
		netUnix, err := procfs.NewNetUnix()
		if err != nil {
			return
		}

		unixPaths := make(map[string]bool, 0)
		for _, row := range netUnix.Rows {
			if !strings.HasPrefix(row.Path, "@") {
				continue
			}
			if !strings.Contains(row.Path, "devtools_remote") {
				continue
			}
			unixPaths[row.Path[1:]] = true
		}

		result := make([]interface{}, 0)
		procs, err := findProcAll(packageName)
		for _, proc := range procs {
			cmdline, _ := proc.CmdLine()
			suffix := "_" + strconv.Itoa(proc.PID)

			for socketPath := range unixPaths {
				if strings.HasSuffix(socketPath, suffix) ||
					(packageName == "com.android.browser" && socketPath == "chrome_devtools_remote") {
					result = append(result, map[string]interface{}{
						"pid":        proc.PID,
						"name":       cmdline[0],
						"socketPath": socketPath,
					})
				}
			}
		}
		renderJSON(w, result)
	})

	m.HandleFunc("/pidof/{pkgname}", func(w http.ResponseWriter, r *http.Request) {
		pkgname := mux.Vars(r)["pkgname"]
		pid, err := pidOf(pkgname)
		if err != nil {
			http.Error(w, err.Error(), http.StatusGone)
			return
		}
		io.WriteString(w, strconv.Itoa(pid))
	})

	m.HandleFunc("/session/{pkgname}", func(w http.ResponseWriter, r *http.Request) {
		packageName := mux.Vars(r)["pkgname"]
		mainActivity, err := mainActivityOf(packageName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusGone) // 410
			return
		}
		// Refs: https://stackoverflow.com/questions/12131555/leading-dot-in-androidname-really-required
		// MainActivity convert to .MainActivity
		// com.example.app.MainActivity keep same
		// app.MainActivity keep same
		// So only words not contains dot, need to add prefix "."
		if !strings.Contains(mainActivity, ".") {
			mainActivity = "." + mainActivity
		}

		flags := r.FormValue("flags")
		if flags == "" {
			flags = "-W -S" // W: wait launched, S: stop before started
		}
		timeout := r.FormValue("timeout") // supported value: 60s, 1m. 60 is invalid
		duration, err := time.ParseDuration(timeout)
		if err != nil {
			duration = 60 * time.Second
		}

		output, err := runShellTimeout(duration, "am", "start", flags, "-n", packageName+"/"+mainActivity)
		if err != nil {
			renderJSON(w, map[string]interface{}{
				"success":      false,
				"error":        err.Error(),
				"output":       string(output),
				"mainActivity": mainActivity,
			})
		} else {
			renderJSON(w, map[string]interface{}{
				"success":      true,
				"mainActivity": mainActivity,
				"output":       string(output),
			})
		}
	}).Methods("POST")

	m.HandleFunc("/session/{pid:[0-9]+}:{pkgname}/{url:ping|jsonrpc/0}", func(w http.ResponseWriter, r *http.Request) {
		pkgname := mux.Vars(r)["pkgname"]
		pid, _ := strconv.Atoi(mux.Vars(r)["pid"])

		proc, err := procfs.NewProc(pid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusGone) // 410
			return
		}
		cmdline, _ := proc.CmdLine()
		if len(cmdline) != 1 || cmdline[0] != pkgname {
			http.Error(w, fmt.Sprintf("cmdline expect [%s] but got %v", pkgname, cmdline), http.StatusGone)
			return
		}
		r.URL.Path = "/" + mux.Vars(r)["url"]
		uiautomatorProxy.ServeHTTP(w, r)
	})

	m.HandleFunc("/shell", func(w http.ResponseWriter, r *http.Request) {
		command := r.FormValue("command")
		if command == "" {
			command = r.FormValue("c")
		}
		timeoutSeconds := r.FormValue("timeout")
		if timeoutSeconds == "" {
			timeoutSeconds = "60"
		}
		seconds, err := strconv.Atoi(timeoutSeconds)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		c := Command{
			Args:    []string{command},
			Shell:   true,
			Timeout: time.Duration(seconds) * time.Second,
		}
		output, err := c.CombinedOutput()
		exitCode := cmdError2Code(err)
		renderJSON(w, map[string]interface{}{
			"output":   string(output),
			"exitCode": exitCode,
			"error":    err,
		})
	}).Methods("GET", "POST")

	// TODO(ssx): untested
	m.HandleFunc("/shell/background", func(w http.ResponseWriter, r *http.Request) {
		command := r.FormValue("command")
		if command == "" {
			command = r.FormValue("c")
		}
		c := Command{
			Args:  []string{command},
			Shell: true,
		}
		pid, err := c.StartBackground()
		if err != nil {
			renderJSON(w, map[string]interface{}{
				"success":     false,
				"description": err.Error(),
			})
			return
		}
		renderJSON(w, map[string]interface{}{
			"success":     true,
			"pid":         pid,
			"description": fmt.Sprintf("Successfully started program: %v", command),
		})
	})

	m.HandleFunc("/shell/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		command := r.FormValue("command")
		if command == "" {
			command = r.FormValue("c")
		}
		c := exec.Command("sh", "-c", command)

		httpWriter := newFakeWriter(func(data []byte) (int, error) {
			n, err := w.Write(data)
			if err == nil {
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			} else {
				log.Println("Write error")
			}
			return n, err
		})
		c.Stdout = httpWriter
		c.Stderr = httpWriter

		// wait until program quit
		cmdQuit := make(chan error, 0)
		go func() {
			cmdQuit <- c.Run()
		}()
		select {
		case <-httpWriter.Err:
			if c.Process != nil {
				c.Process.Signal(syscall.SIGTERM)
			}
		case <-cmdQuit:
			log.Println("command quit")
		}
		log.Println("program quit")
	})

	m.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		log.Println("stop all service")
		service.StopAll()
		log.Println("service stopped")
		io.WriteString(w, "Finished!")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel() // The document says need to call cancel(), but I donot known why.
			server.httpServer.Shutdown(ctx)
		}()
	})

	m.HandleFunc("/services/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := mux.Vars(r)["name"]
		var resp map[string]interface{}
		if !service.Exists(name) {
			w.WriteHeader(400) // bad request
			renderJSON(w, map[string]interface{}{
				"success":     false,
				"description": fmt.Sprintf("service %s does not exist", strconv.Quote(name)),
			})
			return
		}
		switch r.Method {
		case "GET":
			resp = map[string]interface{}{
				"success": true,
				"running": service.Running(name),
			}
		case "POST":
			err := service.Start(name)
			switch err {
			case nil:
				resp = map[string]interface{}{
					"success":     true,
					"description": "successfully started",
				}
			case cmdctrl.ErrAlreadyRunning:
				resp = map[string]interface{}{
					"success":     true,
					"description": "already started",
				}
			default:
				resp = map[string]interface{}{
					"success":     false,
					"description": "failure on start: " + err.Error(),
				}
			}
		case "DELETE":
			err := service.Stop(name)
			switch err {
			case nil:
				resp = map[string]interface{}{
					"success":     true,
					"description": "successfully stopped",
				}
			case cmdctrl.ErrAlreadyStopped:
				resp = map[string]interface{}{
					"success":     true,
					"description": "already stopped",
				}
			default:
				resp = map[string]interface{}{
					"success":     false,
					"description": "failure on stop: " + err.Error(),
				}
			}
		default:
			resp = map[string]interface{}{
				"success":     false,
				"description": "invalid request method: " + r.Method,
			}
		}
		if ok, success := resp["success"].(bool); ok {
			if !success {
				w.WriteHeader(400) // bad request
			}
		}
		renderJSON(w, resp)
	}).Methods("GET", "POST", "DELETE")

	// Deprecated use /services/{name} instead
	m.HandleFunc("/uiautomator", func(w http.ResponseWriter, r *http.Request) {
		err := service.Start("uiautomator")
		if err == nil {
			io.WriteString(w, "Successfully started")
		} else if err == cmdctrl.ErrAlreadyRunning {
			io.WriteString(w, "Already started")
		} else {
			http.Error(w, err.Error(), 500)
		}
	}).Methods("POST")

	// Deprecated
	m.HandleFunc("/uiautomator", func(w http.ResponseWriter, r *http.Request) {
		err := service.Stop("uiautomator", true) // wait until program quit
		if err == nil {
			io.WriteString(w, "Successfully stopped")
		} else if err == cmdctrl.ErrAlreadyStopped {
			io.WriteString(w, "Already stopped")
		} else {
			http.Error(w, err.Error(), 500)
		}
	}).Methods("DELETE")

	// Deprecated
	m.HandleFunc("/uiautomator", func(w http.ResponseWriter, r *http.Request) {
		running := service.Running("uiautomator")
		renderJSON(w, map[string]interface{}{
			"running": running,
		})
	}).Methods("GET")

	m.HandleFunc("/raw/{filepath:.*}", func(w http.ResponseWriter, r *http.Request) {
		filepath := "/" + mux.Vars(r)["filepath"]
		http.ServeFile(w, r, filepath)
	})

	m.HandleFunc("/finfo/{lpath:.*}", func(w http.ResponseWriter, r *http.Request) {
		lpath := "/" + mux.Vars(r)["lpath"]
		finfo, err := os.Stat(lpath)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, err.Error(), 404)
			} else {
				http.Error(w, err.Error(), 403) // forbidden
			}
			return
		}
		data := make(map[string]interface{}, 5)
		data["name"] = finfo.Name()
		data["path"] = lpath
		data["isDirectory"] = finfo.IsDir()
		data["size"] = finfo.Size()

		if finfo.IsDir() {
			files, err := ioutil.ReadDir(lpath)
			if err == nil {
				finfos := make([]map[string]interface{}, 0, 3)
				for _, f := range files {
					finfos = append(finfos, map[string]interface{}{
						"name":        f.Name(),
						"path":        filepath.Join(lpath, f.Name()),
						"isDirectory": f.IsDir(),
					})
				}
				data["files"] = finfos
			}
		}
		renderJSON(w, data)
	})

	// keep ApkService always running
	// if no activity in 5min, then restart apk service
	const apkServiceTimeout = 5 * time.Minute
	apkServiceTimer := NewSafeTimer(apkServiceTimeout)
	go func() {
		for range apkServiceTimer.C {
			log.Println("startservice com.github.uiautomator/.Service")
			runShell("am", "startservice", "-n", "com.github.uiautomator/.Service")
			apkServiceTimer.Reset(apkServiceTimeout)
		}
	}()

	deviceInfo := getDeviceInfo()

	m.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(deviceInfo)
	})

	m.HandleFunc("/info/battery", func(w http.ResponseWriter, r *http.Request) {
		apkServiceTimer.Reset(apkServiceTimeout)
		deviceInfo.Battery.Update()
		// if err := server.tunnel.UpdateInfo(deviceInfo); err != nil {
		// 	io.WriteString(w, "Failure "+err.Error())
		// 	return
		// }
		io.WriteString(w, "Success")
	}).Methods("POST")

	m.HandleFunc("/info/rotation", func(w http.ResponseWriter, r *http.Request) {
		apkServiceTimer.Reset(apkServiceTimeout)
		var direction int                                 // 0,1,2,3
		err := json.NewDecoder(r.Body).Decode(&direction) // TODO: auto get rotation
		if err == nil {
			deviceRotation = direction * 90
			log.Println("rotation change received:", deviceRotation)
		} else {
			rotation, er := androidutils.Rotation()
			if er != nil {
				log.Println("rotation auto get err:", er)
				http.Error(w, "Failure", 500)
				return
			}
			deviceRotation = rotation
		}

		// Kill not controled minicap
		killed := false
		procWalk(func(proc procfs.Proc) {
			executable, _ := proc.Executable()
			if filepath.Base(executable) != "minicap" {
				return
			}
			stat, err := proc.NewStat()
			if err != nil || stat.PPID != 1 { // only not controled minicap need killed
				return
			}
			if p, err := os.FindProcess(proc.PID); err == nil {
				log.Println("Kill", executable)
				p.Kill()
				killed = true
			}
		})
		if killed {
			service.Start("minicap")
		}
		updateMinicapRotation(deviceRotation)

		// APK Service will send rotation to atx-agent when rotation changes
		runShellTimeout(5*time.Second, "am", "startservice", "--user", "0", "-n", "com.github.uiautomator/.Service")
		renderJSON(w, map[string]int{
			"rotation": deviceRotation,
		})
		// fmt.Fprintf(w, "rotation change to %d", deviceRotation)
	})

	/*
	 # URLRules:
	 #   URLPath ends with / means directory, eg: $DEVICE_URL/upload/sdcard/
	 #   The rest means file, eg: $DEVICE_URL/upload/sdcard/a.txt
	 #
	 # Upload a file to destination
	 $ curl -X POST -F file=@file.txt -F mode=0755 $DEVICE_URL/upload/sdcard/a.txt

	 # Upload a directory (file must be zip), URLPath must ends with /
	 $ curl -X POST -F file=@dir.zip -F dir=true $DEVICE_URL/upload/sdcard/atx-stuffs/
	*/
	m.HandleFunc("/upload/{target:.*}", func(w http.ResponseWriter, r *http.Request) {
		target := mux.Vars(r)["target"]
		if runtime.GOOS != "windows" {
			target = "/" + target
		}
		isDir := r.FormValue("dir") == "true"
		var fileMode os.FileMode
		if _, err := fmt.Sscanf(r.FormValue("mode"), "%o", &fileMode); !isDir && err != nil {
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

		var targetDir = target
		if !isDir {
			if strings.HasSuffix(target, "/") {
				target = path.Join(target, header.Filename)
			}
			targetDir = filepath.Dir(target)
		} else {
			if !strings.HasSuffix(target, "/") {
				http.Error(w, "URLPath must endswith / if upload a directory", 400)
				return
			}
		}
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			os.MkdirAll(targetDir, 0755)
		}

		if isDir {
			err = archiver.Zip.Read(file, target)
		} else {
			err = copyToFile(file, target)
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !isDir && fileMode != 0 {
			os.Chmod(target, fileMode)
		}
		if fileInfo, err := os.Stat(target); err == nil {
			fileMode = fileInfo.Mode()
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"target": target,
			"isDir":  isDir,
			"mode":   fmt.Sprintf("0%o", fileMode),
		})
	})

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

	m.HandleFunc("/packages", func(w http.ResponseWriter, r *http.Request) {
		var url = r.FormValue("url")
		filepath := TempFileName("/sdcard/tmp", ".apk")
		key := background.HTTPDownload(url, filepath, 0644)
		go func() {
			defer os.Remove(filepath) // release sdcard space

			state := background.Get(key)
			state.Status = "downloading"
			if err := background.Wait(key); err != nil {
				log.Println("http download error")
				state.Error = err.Error()
				state.Status = "failure"
				state.Message = "http download error"
				return
			}

			state.Status = "installing"
			if err := forceInstallAPK(filepath); err != nil {
				state.Error = err.Error()
				state.Status = "failure"
			} else {
				state.Status = "success"
			}
		}()
		renderJSON(w, map[string]interface{}{
			"success": true,
			"data": map[string]string{
				"id": key,
			},
		})
	}).Methods("POST")

	// id: int
	m.HandleFunc("/packages/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		state := background.Get(id)
		w.Header().Set("Content-Type", "application/json")
		data, _ := json.Marshal(state.Progress)
		renderJSON(w, map[string]interface{}{
			"success": true,
			"data": map[string]string{
				"status":      state.Status,
				"description": string(data),
			},
		})
		json.NewEncoder(w).Encode(state)
	}).Methods("GET")

	m.HandleFunc("/packages", func(w http.ResponseWriter, r *http.Request) {
		pkgs, err := listPackages()
		if err != nil {
			w.WriteHeader(500)
			renderJSON(w, map[string]interface{}{
				"success":     false,
				"description": err.Error(),
			})
			return
		}
		renderJSON(w, pkgs)
	}).Methods("GET")

	m.HandleFunc("/packages/{pkgname}/info", func(w http.ResponseWriter, r *http.Request) {
		pkgname := mux.Vars(r)["pkgname"]
		info, err := readPackageInfo(pkgname)
		if err != nil {
			renderJSON(w, map[string]interface{}{
				"success":     false,
				"description": err.Error(), // "package " + strconv.Quote(pkgname) + " not found",
			})
			return
		}
		renderJSON(w, map[string]interface{}{
			"success": true,
			"data":    info,
		})
	})

	m.HandleFunc("/packages/{pkgname}/icon", func(w http.ResponseWriter, r *http.Request) {
		pkgname := mux.Vars(r)["pkgname"]
		info, err := readPackageInfo(pkgname)
		if err != nil {
			http.Error(w, "package not found", 403)
			return
		}
		if info.Icon == nil {
			http.Error(w, "package not found", 400)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		jpeg.Encode(w, info.Icon, &jpeg.Options{Quality: 80})
	})

	// deprecated
	m.HandleFunc("/install", func(w http.ResponseWriter, r *http.Request) {
		var url = r.FormValue("url")
		var tmpdir = r.FormValue("tmpdir")
		if tmpdir == "" {
			tmpdir = "/data/local/tmp"
		}

		filepath := TempFileName(tmpdir, ".apk")
		key := background.HTTPDownload(url, filepath, 0644)
		go func() {
			defer os.Remove(filepath) // release sdcard space

			state := background.Get(key)
			state.Status = "downloading"
			if err := background.Wait(key); err != nil {
				log.Println("http download error")
				state.Error = err.Error()
				state.Message = "http download error"
				state.Status = "failure"
				return
			}

			state.Message = "installing"
			state.Status = "installing"
			if err := forceInstallAPK(filepath); err != nil {
				state.Error = err.Error()
				state.Message = "error install"
				state.Status = "failure"
			} else {
				state.Message = "success installed"
				state.Status = "success"
			}
		}()
		io.WriteString(w, key)
	}).Methods("POST")

	// deprecated
	m.HandleFunc("/install/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		state := background.Get(id)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)
	}).Methods("GET")

	// deprecated
	m.HandleFunc("/install/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		state := background.Get(id)
		if state.Progress != nil {
			if dproxy, ok := state.Progress.(*downloadProxy); ok {
				dproxy.Cancel()
				io.WriteString(w, "Cancelled")
				return
			}
		}
		io.WriteString(w, "Unable to canceled")
	}).Methods("DELETE")

	// fix minitouch
	m.HandleFunc("/minitouch", func(w http.ResponseWriter, r *http.Request) {
		if err := installMinitouch(); err == nil {
			log.Println("update minitouch success")
			io.WriteString(w, "Update minitouch success")
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}).Methods("PUT")

	m.HandleFunc("/minitouch", func(w http.ResponseWriter, r *http.Request) {
		service.Stop("minitouch", true)
		io.WriteString(w, "minitouch stopped")
	}).Methods("DELETE")

	m.HandleFunc("/minitouch", singleFightNewerWebsocket(func(w http.ResponseWriter, r *http.Request, ws *websocket.Conn) {
		defer ws.Close()
		const wsWriteWait = 10 * time.Second
		wsWrite := func(messageType int, data []byte) error {
			ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
			return ws.WriteMessage(messageType, data)
		}
		wsWrite(websocket.TextMessage, []byte("start @minitouch service"))
		if err := service.Start("minitouch"); err != nil && err != cmdctrl.ErrAlreadyRunning {
			wsWrite(websocket.TextMessage, []byte("@minitouch service start failed: "+err.Error()))
			return
		}
		unixSocketName := "@minitouchagent"
		wsWrite(websocket.TextMessage, []byte("dial unix:"+unixSocketName))
		log.Printf("minitouch connection: %v", r.RemoteAddr)
		retries := 0
		quitC := make(chan bool, 2)
		operC := make(chan TouchRequest, 10)
		defer func() {
			wsWrite(websocket.TextMessage, []byte(unixSocketName+" websocket closed"))
			close(operC)
		}()
		go func() {
			for {
				if retries > 10 {
					log.Printf("unix %s connect failed", unixSocketName)
					wsWrite(websocket.TextMessage, []byte(unixSocketName+" listen timeout, possibly minitouch not installed"))
					ws.Close()
					break
				}
				conn, err := net.Dial("unix", unixSocketName)
				if err != nil {
					retries++
					log.Printf("dial %s error: %v, wait 0.5s", unixSocketName, err)
					select {
					case <-quitC:
						return
					case <-time.After(500 * time.Millisecond):
					}
					continue
				}
				log.Printf("unix %s connected, accepting requests", unixSocketName)
				retries = 0 // connected, reset retries
				err = drainTouchRequests(conn, operC)
				conn.Close()
				if err != nil {
					log.Println("drain touch requests err:", err)
				} else {
					log.Printf("unix %s disconnected", unixSocketName)
					break // operC closed
				}
			}
		}()
		var touchRequest TouchRequest
		for {
			err := ws.ReadJSON(&touchRequest)
			if err != nil {
				log.Println("readJson err:", err)
				quitC <- true
				break
			}
			select {
			case operC <- touchRequest:
			case <-time.After(2 * time.Second):
				wsWrite(websocket.TextMessage, []byte("touch request buffer full"))
			}
		}
	})).Methods("GET")

	// fix minicap
	m.HandleFunc("/minicap", func(w http.ResponseWriter, r *http.Request) {
		if err := installMinicap(); err == nil {
			log.Println("update minicap success")
			io.WriteString(w, "Update minicap success")
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}).Methods("PUT")

	m.HandleFunc("/minicap", singleFightNewerWebsocket(func(w http.ResponseWriter, r *http.Request, ws *websocket.Conn) {
		defer ws.Close()

		const wsWriteWait = 10 * time.Second
		wsWrite := func(messageType int, data []byte) error {
			ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
			return ws.WriteMessage(messageType, data)
		}
		wsWrite(websocket.TextMessage, []byte("restart @minicap service"))
		if err := service.Restart("minicap"); err != nil && err != cmdctrl.ErrAlreadyRunning {
			wsWrite(websocket.TextMessage, []byte("@minicap service start failed: "+err.Error()))
			return
		}

		wsWrite(websocket.TextMessage, []byte("dial unix:@minicap"))
		log.Printf("minicap connection: %v", r.RemoteAddr)
		dataC := make(chan []byte, 10)
		quitC := make(chan bool, 2)

		go func() {
			defer close(dataC)
			retries := 0
			for {
				if retries > 10 {
					log.Println("unix @minicap connect failed")
					dataC <- []byte("@minicap listen timeout, possibly minicap not installed")
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
				dataC <- []byte("rotation " + strconv.Itoa(deviceRotation))
				retries = 0 // connected, reset retries
				if er := translateMinicap(conn, dataC, quitC); er == nil {
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
		for data := range dataC {
			if string(data[:2]) == "\xff\xd8" { // jpeg data
				if err := wsWrite(websocket.BinaryMessage, data); err != nil {
					break
				}
				if err := wsWrite(websocket.TextMessage, []byte("data size: "+strconv.Itoa(len(data)))); err != nil {
					break
				}
			} else {
				if err := wsWrite(websocket.TextMessage, data); err != nil {
					break
				}
			}
		}
		quitC <- true
		log.Println("stream finished")
	})).Methods("GET")

	// TODO(ssx): perfer to delete
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
			// TODO(ssx): runDaemon()
		}()
	})

	m.HandleFunc("/term", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			handleTerminalWebsocket(w, r)
			return
		}
		renderHTML(w, "terminal.html")
	})

	screenshotIndex := -1
	nextScreenshotFilename := func() string {
		targetFolder := "/data/local/tmp/minicap-images"
		if _, err := os.Stat(targetFolder); err != nil {
			os.MkdirAll(targetFolder, 0755)
		}
		screenshotIndex = (screenshotIndex + 1) % 5
		return filepath.Join(targetFolder, fmt.Sprintf("%d.jpg", screenshotIndex))
	}

	m.HandleFunc("/screenshot", func(w http.ResponseWriter, r *http.Request) {
		targetURL := "/screenshot/0"
		if r.URL.RawQuery != "" {
			targetURL += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, targetURL, 302)
	}).Methods("GET")

	m.Handle("/jsonrpc/0", uiautomatorProxy)
	m.Handle("/ping", uiautomatorProxy)
	m.HandleFunc("/screenshot/0", func(w http.ResponseWriter, r *http.Request) {
		download := r.FormValue("download")
		if download != "" {
			w.Header().Set("Content-Disposition", "attachment; filename="+download)
		}

		thumbnailSize := r.FormValue("thumbnail")
		filename := nextScreenshotFilename()

		// android emulator use screencap
		// then minicap when binary and .so exists
		// then uiautomator when service(uiautomator) is running
		// last screencap

		method := "screencap"
		if getCachedProperty("ro.product.cpu.abi") == "x86" { // android emulator
			method = "screencap"
		} else if fileExists("/data/local/tmp/minicap") && fileExists("/data/local/tmp/minicap.so") && r.FormValue("minicap") != "false" && strings.ToLower(getCachedProperty("ro.product.manufacturer")) != "meizu" {
			method = "minicap"
		} else if service.Running("uiautomator") {
			method = "uiautomator"
		}

		var err error
		switch method {
		case "screencap":
			err = screenshotWithScreencap(filename)
		case "minicap":
			err = screenshotWithMinicap(filename, thumbnailSize)
		case "uiautomator":
			uiautomatorProxy.ServeHTTP(w, r)
			return
		}
		if err != nil && method != "screencap" {
			method = "screencap"
			err = screenshotWithScreencap(filename)
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("X-Screenshot-Method", method)
		http.ServeFile(w, r, filename)
	})

	m.HandleFunc("/wlan/ip", func(w http.ResponseWriter, r *http.Request) {
		itf, err := net.InterfaceByName("wlan0")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		addrs, err := itf.Addrs()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, addr := range addrs {
			if v, ok := addr.(*net.IPNet); ok {
				io.WriteString(w, v.IP.String())
			}
			return
		}
		http.Error(w, "wlan0 have no ip address", 500)
	})
	m.Handle("/assets/{(.*)}", http.StripPrefix("/assets", http.FileServer(Assets)))

	var handler = cors.New(cors.Options{
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
	}).Handler(m)
	logHandler := handlers.LoggingHandler(os.Stdout, handler)
	server.httpServer = &http.Server{Handler: logHandler} // url(/stop) need it.
}

func (s *Server) Serve(lis net.Listener) error {
	return s.httpServer.Serve(lis)
}
