package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/shogo82148/androidbinary/apk"
)

var canFixedInstallFails = map[string]bool{
	"INSTALL_FAILED_PERMISSION_MODEL_DOWNGRADE": true,
	"INSTALL_FAILED_UPDATE_INCOMPATIBLE":        true,
	"INSTALL_FAILED_VERSION_DOWNGRADE":          true,
}

type APKManager struct {
	Path         string
	packageName  string
	mainActivity string
}

func (am *APKManager) PackageName() (string, error) {
	if am.packageName != "" {
		return am.packageName, nil
	}
	pkg, err := apk.OpenFile(am.Path)
	if err != nil {
		return "", errors.Wrap(err, "apk parse")
	}
	defer pkg.Close()
	am.packageName = pkg.PackageName()
	am.mainActivity, _ = pkg.MainActivity()

	return am.packageName, nil
}

func (am *APKManager) Install() error {
	sdk, _ := strconv.Atoi(getCachedProperty("ro.build.version.sdk"))
	cmds := []string{"pm", "install", "-d", "-r", am.Path}
	if sdk >= 23 { // android 6.0
		cmds = []string{"pm", "install", "-d", "-r", "-g", am.Path}
	}
	out, err := runShell(cmds...)
	if err != nil {
		matches := regexp.MustCompile(`Failure \[([\w_ ]+)\]`).FindStringSubmatch(string(out))
		if len(matches) > 0 {
			return errors.Wrap(err, matches[0])
		}
		return errors.Wrap(err, string(out))
	}
	return nil
}

func (am *APKManager) ForceInstall() error {
	err := am.Install()
	if err == nil {
		return nil
	}
	errType := regexp.MustCompile(`INSTALL_FAILED_[\w_]+`).FindString(err.Error())
	if !canFixedInstallFails[errType] {
		return err
	}
	log.Infof("install meet %v, try to uninstall", errType)
	packageName, err := am.PackageName()
	if err != nil {
		return errors.Wrap(err, "apk parse")
	}

	log.Infof("uninstall %s", packageName)
	runShell("pm", "uninstall", packageName)
	return am.Install()
}

type StartOptions struct {
	Stop bool
	Wait bool
}

func (am *APKManager) Start(opts StartOptions) error {
	packageName, err := am.PackageName()
	if err != nil {
		return err
	}
	if am.mainActivity == "" {
		return errors.New("parse MainActivity failed")
	}
	mainActivity := am.mainActivity
	if !strings.Contains(mainActivity, ".") {
		mainActivity = "." + mainActivity
	}
	_, err = runShellTimeout(30*time.Second, "am", "start", "-n", packageName+"/"+mainActivity)
	return err
}

func installAPK(path string) error {
	am := &APKManager{Path: path}
	return am.Install()
}

func forceInstallAPK(filepath string) error {
	am := &APKManager{Path: filepath}
	return am.ForceInstall()
}
