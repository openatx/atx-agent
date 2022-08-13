package main

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
)

//func screenshotWithMinicap(filename, thumbnailSize string) (err error) {
//	output, err := runShellOutput("LD_LIBRARY_PATH=/data/local/tmp", "/data/local/tmp/minicap", "-i")
//	if err != nil {
//		return
//	}
//	var f MinicapInfo
//	if er := json.Unmarshal([]byte(output), &f); er != nil {
//		err = fmt.Errorf("minicap not supported: %v", er)
//		return
//	}
//	if thumbnailSize == "" {
//		thumbnailSize = fmt.Sprintf("%dx%d", f.Width, f.Height)
//	}
//	if _, err = runShell(
//		"LD_LIBRARY_PATH=/data/local/tmp",
//		"/data/local/tmp/minicap",
//		"-P", fmt.Sprintf("%dx%d@%s/%d", f.Width, f.Height, thumbnailSize, f.Rotation),
//		"-s", ">"+filename); err != nil {
//		err = errors.Wrap(err, "minicap")
//		return
//	}
//	return nil
//}

func screenshotWithMinicap(filename, thumbnailSize string) (err error) {
	if !isMinicapSupported() {
		err = fmt.Errorf("minicap not supported.")
		return
	}
	devInfo := getDeviceInfo()
	width, height := devInfo.Display.Width, devInfo.Display.Height
	if thumbnailSize == "" {
		thumbnailSize = fmt.Sprintf("%dx%d", width, height)
	}
	if _, err = runShell(
		"LD_LIBRARY_PATH=/data/local/tmp",
		"/data/local/tmp/minicap",
		"-P", fmt.Sprintf("%dx%d@%s/%d", width, height, thumbnailSize, deviceRotation),
		"-s", ">"+filename); err != nil {
		err = errors.Wrap(err, "minicap")
		return
	}
	return nil
}

func screenshotWithScreencap(filename string) (err error) {
	_, err = runShellOutput("screencap", "-p", filename)
	err = errors.Wrap(err, "screencap")
	return
}

//func isMinicapSupported() bool {
//	output, err := runShellOutput("LD_LIBRARY_PATH=/data/local/tmp", "/data/local/tmp/minicap", "-i")
//	if err != nil {
//		return false
//	}
//	var f MinicapInfo
//	if er := json.Unmarshal([]byte(output), &f); er != nil {
//		return false
//	}
//	output, err = runShell(
//		"LD_LIBRARY_PATH=/data/local/tmp",
//		"/data/local/tmp/minicap",
//		"-P", fmt.Sprintf("%dx%d@%dx%d/%d", f.Width, f.Height, f.Width, f.Height, f.Rotation),
//		"-s", "2>/dev/null")
//	if err != nil {
//		return false
//	}
//	return bytes.Equal(output[:2], []byte("\xff\xd8")) // JpegFormat
//}

/*
检查minicap是否可用:
使用 -t 来检测minicap是否可用，而非使用 -i，原因如下：

-t : Attempt to get the capture method running, then exit.
-i : Get display information in JSON format. May segfault.  // -i 在有些手机上会出现segfault 139错误

 */
func isMinicapSupported() bool {
	devInfo := getDeviceInfo()
	width, height := devInfo.Display.Width, devInfo.Display.Height
	output, err := runShellOutput(
		"LD_LIBRARY_PATH=/data/local/tmp",
		"/data/local/tmp/minicap",
		"-P", fmt.Sprintf("%dx%d@%dx%d/%d", width, height, width, height, deviceRotation),
		"-t")
	if err != nil {
		return false
	}
	outputs := strings.Split(string(output), "\n")
	if len(outputs) < 2{
		return false
	}
	status := outputs[len(outputs) - 2]
	return status == "OK"
}
