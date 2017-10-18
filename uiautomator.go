package main

import (
	"log"
	"os"
	"os/exec"
	"runtime"
	"time"
)

func safeRunUiautomator() {
	if runtime.GOOS == "windows" {
		return
	}
	retry := 5
	for retry > 0 {
		retry--
		start := time.Now()
		if err := runUiautomator(); err != nil {
			log.Printf("uiautomator quit: %v", err)
		}
		if time.Since(start) > 1*time.Minute {
			retry = 5
		}
		time.Sleep(2 * time.Second)
	}
	log.Println("uiautomator can not started")
}

func runUiautomator() error {
	c := exec.Command("am", "instrument", "-w", "-r",
		"-e", "debug", "false",
		"-e", "class", "com.github.uiautomator.stub.Stub",
		"com.github.uiautomator.test/android.support.test.runner.AndroidJUnitRunner")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
