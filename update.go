package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/codeskyblue/goreq"
	"github.com/getlantern/go-update"
	"github.com/mholt/archiver"
	"github.com/mitchellh/ioprogress"
)

func formatString(format string, params map[string]string) string {
	for k, v := range params {
		format = strings.Replace(format, "{"+k+"}", v, -1)
	}
	return format
}

func makeTempDir() string {
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm" {
		target := "/data/local/tmp/atx-update.tmp"
		os.MkdirAll(target, 0755)
		return target
	}
	os.MkdirAll("atx-update.tmp", 0755)
	return "atx-update.tmp"
}

func getLatestVersion() (version string, err error) {
	res, err := goreq.Request{
		Uri: fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo),
	}.WithHeader("Authorization", "token e83785ff4e37c67098efcea923b668f4135d1dda").Do() // this GITHUB_TOKEN is only for get lastest version
	if err != nil {
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http status code is not 200, got %d", res.StatusCode)
	}
	var t = struct {
		TagName string `json:"tag_name"`
	}{}
	if err = json.NewDecoder(res.Body).Decode(&t); err != nil {
		return
	}
	if t.TagName == "" {
		return "", errors.New("TagName empty")
	}
	return t.TagName, nil
}

func getChecksums(version string) (map[string]string, error) {
	uri := formatString("https://github.com/{owner}/{repo}/releases/download/{version}/{repo}_{version}_checksums.txt", map[string]string{
		"version": version,
		"owner":   owner,
		"repo":    repo,
	})
	res, err := goreq.Request{
		Uri:             uri,
		MaxRedirects:    10,
		RedirectHeaders: true,
	}.Do()
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	scanner := bufio.NewScanner(res.Body)
	m := make(map[string]string, 6)
	for scanner.Scan() {
		var filename, sha256sum string
		_, err := fmt.Sscanf(scanner.Text(), "%s\t%s", &sha256sum, &filename)
		if err != nil {
			continue
		}
		m[filename] = sha256sum
	}
	return m, nil
}

func doUpdate(version string) (err error) {
	if version == "" {
		version, err = getLatestVersion()
		if err != nil {
			return err
		}
	}
	arch := runtime.GOARCH
	if runtime.GOOS == "linux" && arch == "arm" {
		arch += "v7"
	}
	filename := fmt.Sprintf("%s_%s_%s_%s.tar.gz", repo, version, runtime.GOOS, arch)
	log.Printf("update file: %s", filename)
	checksums, err := getChecksums(version)
	if err != nil {
		return err
	}
	checksum, ok := checksums[filename]
	if !ok {
		return fmt.Errorf("checksums not found for file: %s", filename)
	}
	// fixed get latest version
	uri := formatString("https://github.com/{owner}/{repo}/releases/download/{version}/{filename}", map[string]string{
		"version":  version,
		"owner":    owner,
		"repo":     repo,
		"filename": filename,
	})
	log.Printf("update url: %s", uri)
	res, err := goreq.Request{
		Uri:             uri,
		MaxRedirects:    10,
		RedirectHeaders: true,
	}.Do()
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		err = fmt.Errorf("HTTP download error: [%d] %s", res.StatusCode, res.Status)
		return err
	}
	contentLength, err := strconv.Atoi(res.Header.Get("Content-Length"))
	if err != nil {
		return err
	}
	hasher := sha256.New()
	progressR := &ioprogress.Reader{
		Reader:   res.Body,
		Size:     int64(contentLength),
		DrawFunc: ioprogress.DrawTerminalf(os.Stdout, ioprogress.DrawTextFormatBytes),
	}
	tmpdir := makeTempDir()
	distPath := filepath.Join(tmpdir, "dist.tar.gz")
	f, err := os.Create(distPath)
	if err != nil {
		return err
	}
	writer := io.MultiWriter(f, hasher)
	io.Copy(writer, progressR)
	if err = f.Close(); err != nil {
		return err
	}
	realChecksum := hex.EncodeToString(hasher.Sum(nil))
	if realChecksum != checksum {
		return fmt.Errorf("update file checksum wrong, expected: %s, got: %s", checksum, realChecksum)
	}
	if err = archiver.TarGz.Open(distPath, tmpdir); err != nil {
		return err
	}
	log.Println("perform updating")
	err, _ = update.New().FromFile(filepath.Join(tmpdir, repo))
	return err
}
