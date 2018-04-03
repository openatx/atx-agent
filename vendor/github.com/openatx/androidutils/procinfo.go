package androidutils

import (
	"errors"
	"io/ioutil"
	"regexp"
	"strconv"
	"strings"
)

// MemoryInfo read from /proc/meminfo, unit kB
func MemoryInfo() (info map[string]int, err error) {
	data, err := ioutil.ReadFile("/proc/meminfo")
	if err != nil {
		return
	}
	return parseMemoryInfo(data)
}

func parseMemoryInfo(data []byte) (info map[string]int, err error) {
	re := regexp.MustCompile(`([\w_\(\)]+):\s*(\d+) kB`)
	matches := re.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		return nil, errors.New("Invalid memory info data")
	}
	info = make(map[string]int)
	for _, m := range matches {
		var key = m[1]
		val, _ := strconv.Atoi(m[2])
		info[key] = val
	}
	return
}

type Processor struct {
	Index    int
	BogoMIPS string
	Features []string
}

// ProcessorInfo read from /proc/cpuinfo
func ProcessorInfo() (hardware string, processors []Processor, err error) {
	data, err := ioutil.ReadFile("/proc/cpuinfo")
	if err != nil {
		return
	}
	return parseCpuInfo(data)
}

func parseCpuInfo(data []byte) (hardware string, processors []Processor, err error) {
	re := regexp.MustCompile(`([\w ]+\w)\s*:\s*(.+)`)
	ms := re.FindAllStringSubmatch(string(data), -1)
	processors = make([]Processor, 0, 8)
	var processor = Processor{Index: -1}
	for _, m := range ms {
		var key = m[1]
		var val = m[2]
		if key == "Hardware" { // Hardware occur at last
			hardware = val
			processors = append(processors, processor)
			break
		}
		if key == "processor" {
			idx, _ := strconv.Atoi(val)
			if idx != processor.Index && processor.Index != -1 {
				processors = append(processors, processor)
			}
			processor.Index = idx
			continue
		}
		switch key {
		case "BogoMIPS":
			processor.BogoMIPS = val
		case "Features":
			processor.Features = strings.Split(val, " ")
		default:
			// ignore
		}
	}
	if len(processors) == 0 {
		err = errors.New("Invalid cpuinfo data")
	}
	return
}
