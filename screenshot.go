package main

import (
	"encoding/json"
	"fmt"

	"github.com/pkg/errors"
)

func screenshotWithMinicap(filename, thumbnailSize string) (err error) {
	output, err := runShellOutput("LD_LIBRARY_PATH=/data/local/tmp", "/data/local/tmp/minicap", "-i")
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
		"LD_LIBRARY_PATH=/data/local/tmp",
		"/data/local/tmp/minicap",
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
