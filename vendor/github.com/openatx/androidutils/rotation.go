package androidutils

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// Rotation return degree of screen orientation. 0, 90, 180, 270
func Rotation() (rotation int, err error) {
	rotation, err = getRotationMinicap("")
	if err == nil {
		return
	}
	rotation, err = getRotationDumpsysInput("")
	return
}

type minicapInfo struct {
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Rotation int     `json:"rotation"`
	Density  float32 `json:"density"`
}

// Get rotation through minicap
func getRotationMinicap(output string) (rotation int, err error) {
	if output == "" {
		output, err = runShell("LD_LIBRARY_PATH=/data/local/tmp", "/data/local/tmp/minicap", "-i")
		if err != nil {
			return
		}
	}
	var f minicapInfo
	if er := json.Unmarshal([]byte(output), &f); er != nil {
		err = fmt.Errorf("minicap not supported: %v", er)
		return
	}
	return f.Rotation, nil
}

// Note: On Xiaomi 5. There are two line contains "SurfaceOrientation: ", the first is wrong
func getRotationDumpsysInput(output string) (rotation int, err error) {
	if output == "" {
		output, err = runShell("dumpsys", "input")
		if err != nil {
			return
		}
	}
	pattern := regexp.MustCompile(`SurfaceOrientation:\s*(\d)`)
	matches := pattern.FindStringSubmatch(output)
	if matches == nil {
		return 0, errors.New("dumpsys input has not contains SurfaceOrientation")
	}
	rotation, err = strconv.Atoi(matches[1])
	rotation *= 90
	return
}
