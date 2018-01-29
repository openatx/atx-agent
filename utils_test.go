package main

import (
	"os"
	"testing"
)

func TestTempFileName(t *testing.T) {
	tmpDir := os.TempDir()
	filename := TempFileName(tmpDir, ".apk")
	t.Log(filename)
}
