package collector

import (
	"fmt"
	"os/exec"
	"strings"
)

func parseInt(s string) int {
	var a int
	fmt.Sscanf(s, "%d", &a)
	return a
}

func (SB *ScreenBrightness) getScreenBrightness() error {
	cmd := exec.Command("sh", "-c", "cat /sys/class/backlight/panel0-backlight/brightness")
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	num := parseInt(string(out))
	SB.Number = num
	return nil
}

func (SO *ScreenOn) getScreenOn() error {
	cmd := exec.Command("sh", "-c", "cat /sys/class/backlight/panel0-backlight/bl_power")
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	On := parseInt(strings.TrimSpace(string(out)))
	SO.mScreenOn = On
	// 屏幕状态为关闭需要唤醒
	if On == 4 {
		cmd := exec.Command("sh", "-c", "input keyevent 26")
		_, err := cmd.Output()
		if err != nil {
			return err
		}
	}
	return nil
}
