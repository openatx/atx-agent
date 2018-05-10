package androidutils

import (
	"errors"
	"io/ioutil"
	"regexp"
	"strconv"
	"strings"
)

func HWAddrWLAN() (string, error) {
	macaddr, err := getHWAddrWLAN()
	return strings.ToLower(macaddr), err
}

// WLAN Hardware Address, also known as wlan0 mac address
// Thanks to this article https://android.stackexchange.com/questions/142606/how-can-i-find-my-mac-address/142630#142630
func getHWAddrWLAN() (string, error) {
	// method 1
	if macaddr := CachedProperty("ro.boot.wifimacaddr"); macaddr != "" {
		return macaddr, nil
	}
	// method 2
	data, err := ioutil.ReadFile("/sys/class/net/wlan0/address")
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	// method 3
	output, err := runShell("ip", "address", "show", "wlan0")
	if err != nil {
		return "", err
	}
	matches := regexp.MustCompile(`link/ether ([\w\d:]{17})`).FindStringSubmatch(output)
	if matches == nil {
		return "", errors.New("no mac address founded")
	}
	return matches[1], nil
}

type Display struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// WindowSize parse command "wm size" output
// command output example:
//   Physical size: 1440x2560
//   Override size: 1080x1920
func WindowSize() (display Display, err error) {
	output, err := runShell("wm", "size")
	if err != nil {
		return
	}
	w, h, err := parseWmSize(output)
	return Display{w, h}, err
}

var wmSizePattern = regexp.MustCompile(`(\w+)\s+size:\s+(\d+)x(\d+)`)

func parseWmSize(output string) (width, height int, err error) {
	ms := wmSizePattern.FindAllStringSubmatch(output, -1)
	if len(ms) == 0 {
		err = errors.New("wm size return unrecognize output: " + output)
		return
	}
	if len(ms) == 2 {
		width, _ = strconv.Atoi(ms[1][2])
		height, _ = strconv.Atoi(ms[1][3])
		return width, height, nil
	}
	width, _ = strconv.Atoi(ms[0][2])
	height, _ = strconv.Atoi(ms[0][3])
	return
}
