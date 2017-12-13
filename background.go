/*
Handle offline download and apk install
*/
package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/DeanThompson/syncmap"
)

type BackgroundStatus struct {
	Message string `json:"message"`
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

func (b *Background) InstallApk(filepath string) (key string) {
	return
}

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

func (b *Background) HTTPDownload(urlStr string, dst string) (key string) {
	key = b.genKey()
	go b.doHTTPDownload(urlStr, dst, key)
	return
}

func (b *Background) doHTTPDownload(urlStr string, dst string, key string) {
	// TODO
	defer b.delayDelete(key)
}
