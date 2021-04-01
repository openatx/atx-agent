package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/openatx/androidutils"
)

const (
	apkVersionName = "2.3.3"
)

func init() {
	go installRequirements()
}

func installRequirements() error {
	// log.Println("install uiautomator apk")
	// if err := installUiautomatorAPK(); err != nil {
	// 	return err
	// }
	log.Println("install minitouch")
	if err := installMinitouch(); err != nil {
		return err
	}

	log.Println("install minicap")
	return installMinicap()
}

func installUiautomatorAPK() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if checkUiautomatorInstalled() {
		return nil
	}
	appDebug := filepath.Join(expath, "app-uiautomator.apk")
	appDebugURL := formatString("http://{baseurl}/uiautomator/{version}/{apk}", map[string]string{
		"baseurl": baseurl,
		"version": apkVersionName,
		"apk":     "app-uiautomator.apk",
	})
	if _, err := httpDownload(appDebug, appDebugURL, 0644); err != nil {
		return err
	}
	if err := forceInstallAPK(appDebug); err != nil {
		return err
	}

	appDebugTest := filepath.Join(expath, "app-uiautomator-test.apk")
	appDebugTestURL := formatString("http://{baseurl}/uiautomator/{version}/{apk}", map[string]string{
		"baseurl": baseurl,
		"version": apkVersionName,
		"apk":     "app-uiautomator-test.apk",
	})
	if _, err := httpDownload(appDebugTest, appDebugTestURL, 0644); err != nil {
		return err
	}
	if err := forceInstallAPK(appDebugTest); err != nil {
		return err
	}

	return nil
}

func installMinicap() error {
	minicapbin := filepath.Join(expath, "minicap")
	minicapso := filepath.Join(expath, "minicap.so")
	if fileExists(minicapbin) && fileExists(minicapso) {
		return nil
	}
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

	abi := getCachedProperty("ro.product.cpu.abi")
	sdk := getCachedProperty("ro.build.version.sdk")
	pre := getCachedProperty("ro.build.version.preview_sdk")
	if pre != "" && pre != "0" {
		sdk = sdk + pre
	}

	binURL := formatString("http://{baseurl}/{path}/{abi}/{bin}", map[string]string{
		"baseurl": baseurl,
		"path":    "stf-binaries/node_modules/@devicefarmer/minicap-prebuilt/prebuilt",
		"abi":     abi,
		"bin":     "bin/minicap",
	})

	//binURL := strings.Join([]string{minicapSource, abi, "bin", "minicap"}, "/")
	_, err := httpDownload(minicapbin, binURL, 0755)
	if err != nil {
		return err
	}

	libURL := formatString("http://{baseurl}/{path}/{abi}/lib/{lib}/{so}", map[string]string{
		"baseurl": baseurl,
		"path":    "stf-binaries/node_modules/@devicefarmer/minicap-prebuilt/prebuilt",
		"abi":     abi,
		"lib":     "android-" + sdk,
		"so":      "minicap.so",
	})
	//libURL := strings.Join([]string{minicapSource, abi, "lib", "android-" + sdk, "minicap.so"}, "/")
	_, err = httpDownload(minicapso, libURL, 0644)
	if err != nil {
		return err
	}
	return nil
}

func installMinitouch() error {
	minitouchbin := filepath.Join(expath, "minitouch")
	if fileExists(minitouchbin) {
		return nil
	}
	binURL := formatString("http://{baseurl}/{path}/{abi}/{bin}", map[string]string{
		"baseurl": baseurl,
		"path":    "stf-binaries/node_modules/@devicefarmer/minitouch-prebuilt/prebuilt",
		"abi":     getCachedProperty("ro.product.cpu.abi"),
		"bin":     "bin/minitouch",
	})
	_, err := httpDownload(minitouchbin, binURL, 0755)
	return err
}

func checkUiautomatorInstalled() (ok bool) {
	if pi, err := androidutils.StatPackage("com.github.uiautomator"); err == nil {
		fmt.Printf("uiautomator\nversion name:(%s)\nversion code:(%d)",
			pi.Version.Name, pi.Version.Code)
	} else {
		return false
	}
	if pi, err := androidutils.StatPackage("com.github.uiautomator.test"); err == nil {
		fmt.Printf("uiautomator test\nversion name:(%s)\nversion code:(%d)",
			pi.Version.Name, pi.Version.Code)
	} else {
		return false
	}
	return true
}
