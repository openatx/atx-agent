package androidutils

import (
	"errors"
	"regexp"
	"strconv"
)

var (
	ErrPackageNotExist = errors.New("package not exist")
)

var (
	rePkgPath = regexp.MustCompile(`codePath=([^\s]+)`)
	reVerCode = regexp.MustCompile(`versionCode=(\d+)`)
	reVerName = regexp.MustCompile(`versionName=([^\s]+)`)
)

type PackageInfo struct {
	Name    string
	Path    string
	Version struct {
		Code int
		Name string
	}
}

// StatPackage returns PackageInfo
// If package not found, err will be ErrPackageNotExist
func StatPackage(packageName string) (pi PackageInfo, err error) {
	pi.Name = packageName
	out, err := runShell("dumpsys", "package", packageName)
	if err != nil {
		return
	}

	matches := rePkgPath.FindStringSubmatch(out)
	if len(matches) == 0 {
		err = ErrPackageNotExist
		return
	}
	pi.Path = matches[1]

	matches = reVerCode.FindStringSubmatch(out)
	if len(matches) == 0 {
		err = ErrPackageNotExist
		return
	}
	pi.Version.Code, _ = strconv.Atoi(matches[1])

	matches = reVerName.FindStringSubmatch(out)
	if len(matches) == 0 {
		err = ErrPackageNotExist
		return
	}
	pi.Version.Name = matches[1]
	return
}
