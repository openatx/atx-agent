/*
Handle offline download and apk install
*/
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/DeanThompson/syncmap"
	"github.com/franela/goreq"
)

var background = &Background{
	sm: syncmap.New(),
}

type BackgroundStatus struct {
	Message  string      `json:"message"`
	Progress interface{} `json:"progress"`
}

type Background struct {
	sm *syncmap.SyncMap
	n  int
	mu sync.Mutex
}

// Get return nil if not found
func (b *Background) Get(key string) (status *BackgroundStatus) {
	value, ok := b.sm.Get(key)
	if !ok {
		return nil
	}
	return value.(*BackgroundStatus)
}

// func (b *Background) InstallApk(filepath string) (key string) {
// 	return
// }

func (b *Background) genKey() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.n++
	key := fmt.Sprintf("%d", b.n)
	b.sm.Set(key, &BackgroundStatus{})
	return key
}

func (b *Background) delayDelete(key string) {
	go func() {
		time.Sleep(5 * time.Minute)
		b.sm.Delete(key)
	}()
}

func (b *Background) HTTPDownload(urlStr string, dst string, mode os.FileMode) (key string) {
	key = b.genKey()
	go func() {
		defer b.delayDelete(key)
		b.Get(key).Message = "downloading"
		if err := b.doHTTPDownload(urlStr, dst, key, mode); err != nil {
			b.Get(key).Message = "error: " + err.Error()
		} else {
			b.Get(key).Message = "downloaded"
		}
	}()
	return
}

func (b *Background) doHTTPDownload(urlStr string, dst string, key string, fileMode os.FileMode) (err error) {
	res, err := goreq.Request{
		Uri:             urlStr,
		MaxRedirects:    10,
		RedirectHeaders: true,
	}.Do()
	if err != nil {
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, err := res.Body.ToString()
		if err != nil && err == bytes.ErrTooLarge {
			return fmt.Errorf("Expected HTTP Status code: %d", res.StatusCode)
		}
		return errors.New(body)
	}

	file, err := os.Create(dst)
	if err != nil {
		return
	}
	defer file.Close()

	var totalSize int
	fmt.Sscanf(res.Header.Get("Content-Length"), "%d", &totalSize)
	wrproxy := newDownloadProxy(file, totalSize)
	defer wrproxy.Done()
	b.Get(key).Progress = wrproxy

	_, err = io.Copy(wrproxy, res.Body)
	if err != nil {
		return err
	}
	if fileMode != 0 {
		os.Chmod(dst, fileMode)
	}
	return
}
