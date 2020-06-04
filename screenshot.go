package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/pkg/errors"
)

func Screenshot(filename string, thumbnailSize string) (err error) {
	ldlibrarypath := fmt.Sprintf("LD_LIBRARY_PATH=%v", expath)
	minicapbin := fmt.Sprintf("%v/%v", expath, "minicap")
	output, err := runShellOutput(ldlibrarypath, minicapbin, "-i")
	if err != nil {
		return
	}
	var f MinicapInfo
	if er := json.Unmarshal([]byte(output), &f); er != nil {
		err = fmt.Errorf("minicap not supported: %v", er)
		return
	}
	if thumbnailSize == "" {
		thumbnailSize = fmt.Sprintf("%dx%d", f.Width, f.Height)
	}
	if _, err = runShell(
		ldlibrarypath,
		minicapbin,
		"-P", fmt.Sprintf("%dx%d@%s/%d", f.Width, f.Height, thumbnailSize, f.Rotation),
		"-s", ">"+filename); err != nil {
		return
	}
	return nil
}
func screenshotWithMinicap(filename, thumbnailSize string) (err error) {
	output, err := runShellOutput(fmt.Sprintf("LD_LIBRARY_PATH=%v", expath), fmt.Sprintf("%v/%v", expath, "minicap"), "-i")
	if err != nil {
		return
	}
	var f MinicapInfo
	if er := json.Unmarshal([]byte(output), &f); er != nil {
		err = fmt.Errorf("minicap not supported: %v", er)
		return
	}
	if thumbnailSize == "" {
		thumbnailSize = fmt.Sprintf("%dx%d", f.Width, f.Height)
	}
	if _, err = runShell(
		fmt.Sprintf("LD_LIBRARY_PATH=%v", expath),
		fmt.Sprintf("%v/%v", expath, "minicap"),
		"-P", fmt.Sprintf("%dx%d@%s/%d", f.Width, f.Height, thumbnailSize, f.Rotation),
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

func isMinicapSupported() bool {
	output, err := runShellOutput(fmt.Sprintf("LD_LIBRARY_PATH=%v", expath), fmt.Sprintf("%v/%v", expath, "minicap"), "-i")
	if err != nil {
		return false
	}
	var f MinicapInfo
	if er := json.Unmarshal([]byte(output), &f); er != nil {
		return false
	}
	output, err = runShell(
		fmt.Sprintf("LD_LIBRARY_PATH=%v", expath),
		fmt.Sprintf("%v/%v", expath, "minicap"),
		"-P", fmt.Sprintf("%dx%d@%dx%d/%d", f.Width, f.Height, f.Width, f.Height, f.Rotation),
		"-s", "2>/dev/null")
	if err != nil {
		return false
	}
	return bytes.Equal(output[:2], []byte("\xff\xd8")) // JpegFormat
}
