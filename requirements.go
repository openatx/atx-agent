package main

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/openatx/androidutils"
)

const (
	apkVersionCode = 4
	apkVersionName = "1.0.4"
)

func installRequirements() error {
	log.Println("install uiautomator apk")
	if err := installUiautomatorAPK(); err != nil {
		return err
	}
	return installMinicap()
}

func installUiautomatorAPK() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if checkUiautomatorInstalled() {
		return nil
	}
	baseURL := filepath.Join(baseurl, apkVersionName)
	appdebug := filepath.Join(expath, "app-debug.apk")
	appdebugtest := filepath.Join(expath, "app-debug-test.apk")
	filepath.Join(expath, "app-debug.apk")
	if _, err := httpDownload(appdebug, baseURL+"/app-uiautomator.apk", 0644); err != nil {
		return err
	}
	if _, err := httpDownload(appdebugtest, baseURL+"/app-uiautomator-test.apk", 0644); err != nil {
		return err
	}
	if err := forceInstallAPK(appdebug); err != nil {
		return err
	}
	if err := forceInstallAPK(appdebugtest); err != nil {
		return err
	}
	return nil
}

func installMinicap() error {
	minicapbin := filepath.Join(expath, "mincap")
	minicapso := filepath.Join(expath, "minicap.so")
	if runtime.GOOS == "windows" {
		return nil
	}
	log.Println("install minicap")

	if fileExists(minicapbin) && fileExists(minicapso) {
		if err := Screenshot("/dev/null", ""); err != nil {
			log.Println("err:", err)
		} else {
			return nil
		}
	}
	// remove first to prevent "text file busy"
	os.Remove(minicapbin)
	os.Remove(minicapso)

	minicapSource := filepath.Join(baseurl, "stf-binaries/node_modules/minicap-prebuilt/prebuilt")
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
	_, err = httpDownload(minicapbin, binURL, 0755)
	if err != nil {
		return err
	}
	libURL := strings.Join([]string{minicapSource, abi, "lib", "android-" + sdk, "minicap.so"}, "/")
	_, err = httpDownload(minicapso, libURL, 0644)
	if err != nil {
		return err
	}
	return nil
}

func installMinitouch() error {
	minitouchbin := filepath.Join(expath, "minitouch")
	baseURL := filepath.Join(baseurl, "stf-binaries/node_modules/minitouch-prebuilt/prebuilt")
	abi := getCachedProperty("ro.product.cpu.abi")
	binURL := strings.Join([]string{baseURL, abi, "bin/minitouch"}, "/")
	_, err := httpDownload(minitouchbin, binURL, 0755)
	return err
}

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
