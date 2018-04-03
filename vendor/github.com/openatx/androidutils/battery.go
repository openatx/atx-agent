package androidutils

import (
	"fmt"
	"os/exec"
	"regexp"
)

// ref: http://android-test-tw.blogspot.jp/2012/10/dumpsys-information-android-open-source.html

const (
	BatteryStatusUnknown     = 1
	BatteryStatusCharging    = 2
	BatteryStatusDischarging = 3
	BatteryStatusNotCharging = 4
	BatteryStatusFull        = 5

	BatteryHealthUnknown            = 1
	BatteryHealthGood               = 2
	BatteryHealthOverheat           = 3
	BatteryHealthDead               = 4
	BatteryHealthOverVoltage        = 5
	BatteryHealthUnspecifiedFailure = 6
	BatteryHealthCold               = 7
)

type Battery struct {
	ACPowered       bool   `json:"acPowered"`
	USBPowered      bool   `json:"usbPowered"`
	WirelessPowered bool   `json:"wirelessPowered"`
	Status          int    `json:"status"`
	Health          int    `json:"health"`
	Present         bool   `json:"present"`
	Level           int    `json:"level"`
	Scale           int    `json:"scale"`
	Voltage         int    `json:"voltage"`
	Temperature     int    `json:"temperature"`
	Technology      string `json:"technology"`
}

func (self *Battery) StatusName() string {
	var ss = []string{"unknown", "charging", "discharging", "notcharging", "full"}
	if self.Status < 1 || self.Status > 5 {
		return "unknown"
	}
	return ss[self.Status-1]
}

func dumpsysCommand(args ...string) ([]byte, error) {
	cmd := exec.Command("dumpsys")
	cmd.Args = append(cmd.Args, args...)
	return cmd.Output()
}

func parseBool(s string) bool {
	return s == "true"
}

func parseInt(s string) int {
	var a int
	fmt.Sscanf(s, "%d", &a)
	return a
}

func (self *Battery) Update() error {
	out, err := dumpsysCommand("battery")
	if err != nil {
		return err
	}
	//log.Println(string(out))
	patten := regexp.MustCompile(`(\w+[\w ]*\w+):\s*([-\w\d]+)(\r|\n)`)
	ms := patten.FindAllStringSubmatch(string(out), -1)
	exists := make(map[string]bool)
	for _, fields := range ms {
		var key, val = fields[1], fields[2]

		// filter duplicate items.
		// will happen on phone: 红米Note3
		if exists[key] {
			continue
		}
		exists[key] = true

		switch key {
		case "AC powered":
			self.ACPowered = parseBool(val)
		case "USB powered":
			self.USBPowered = parseBool(val)
		case "Wireless powered":
			self.WirelessPowered = parseBool(val)
		case "status":
			self.Status = parseInt(val)
		case "present":
			self.Present = parseBool(val)
		case "level":
			self.Level = parseInt(val)
		case "scale":
			self.Scale = parseInt(val)
		case "voltage":
			self.Voltage = parseInt(val)
		case "temperature":
			self.Temperature = parseInt(val)
		case "technology":
			self.Technology = val
		}
	}
	return nil
}
