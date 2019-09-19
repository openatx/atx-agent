package main

import (
	"sync"
	"testing"
	"time"
)

func TestSafeTimer(tt *testing.T) {
	deadtime := time.Now().Add(2 * time.Second)
	t := NewSafeTimer(100 * time.Hour)
	wg := sync.WaitGroup{}
	wg.Add(8)
	for i := 0; i < 8; i++ {
		go func() {
			for {
				t.Reset(10 * time.Hour)
				if time.Now().After(deadtime) {
					break
				}
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestSafeTimerUsage(tt *testing.T) {
	t := NewSafeTimer(500 * time.Millisecond)
	done := make(chan bool, 1)
	go func() {
		for range t.C {
			done <- true
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		tt.Fatal("Should accept signal, but got nothing")
	}

	t.Reset()
	select {
	case <-done:
	case <-time.After(time.Second):
		tt.Fatal("Should accept signal, but got nothing")
	}

	t.Reset()
	t.Reset()
	select {
	case <-done:
	case <-time.After(time.Second):
		tt.Fatal("Should accept signal, but got nothing")
	}
	select {
	case <-done:
		tt.Fatal("Should accept nothing, because already accept someting")
	case <-time.After(time.Second):
	}

	t.Stop()
	select {
	case <-done:
		tt.Fatal("Should not accept for timer already stopped")
	case <-time.After(time.Second):
	}
}

// func TestMustPanic(tt *testing.T) {
// 	defer func() {
// 		if r := recover(); r == nil {
// 			tt.Errorf("The code did not panic")
// 		}
// 	}()
// 	deadtime := time.Now().Add(2 * time.Second)
// 	t := time.NewTimer(100 * time.Hour)
// 	wg := sync.WaitGroup{}
// 	wg.Add(8)
// 	for i := 0; i < 8; i++ {
// 		go func() {
// 			for {
// 				t.Reset(10 * time.Hour)
// 				if time.Now().After(deadtime) {
// 					break
// 				}
// 			}
// 			wg.Done()
// 		}()
// 	}
// 	wg.Wait()
// }
