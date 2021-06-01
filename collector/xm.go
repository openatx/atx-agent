package collector

import (
	"fmt"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/openatx/androidutils"
)

type XmCollector struct {
	metric []typedDesc
	logger log.Logger
}

type ScreenBrightness struct {
	Number int
}

type ScreenOn struct {
	mScreenOn int
}

func init() {
	registerCollector("xm", defaultEnabled, NewXmCollector)
}

// NewLoadavgCollector returns a new Collector exposing load average stats.
func NewXmCollector(logger log.Logger) (Collector, error) {
	return &XmCollector{
		metric: []typedDesc{
			{prometheus.NewDesc("xm_device_battery_level", "XM device battery level 0-100", nil, nil), prometheus.GaugeValue},
			{prometheus.NewDesc("xm_device_temperature", "XM device temperature", nil, nil), prometheus.GaugeValue},
			{prometheus.NewDesc("xm_device_battery_status", "XM device battery status", nil, nil), prometheus.GaugeValue},
			{prometheus.NewDesc("xm_device_battery_health", "XM device battery health", nil, nil), prometheus.GaugeValue},
			{prometheus.NewDesc("xm_device_battery_voltage", "XM device battery voltage", nil, nil), prometheus.GaugeValue},
			{prometheus.NewDesc("xm_device_battery_ac_powered", "XM device battery ac powered", nil, nil), prometheus.GaugeValue},
			{prometheus.NewDesc("xm_device_screen_brightness", "XM device screen brightness", nil, nil), prometheus.GaugeValue},
			{prometheus.NewDesc("xm_device_screen_on", "XM device screen on", nil, nil), prometheus.GaugeValue},
		},
		logger: logger,
	}, nil
}

func (c *XmCollector) Update(ch chan<- prometheus.Metric) error {
	battery := androidutils.Battery{}
	err := battery.Update()
	if err != nil {
		return fmt.Errorf("couldn't get battery: %w", err)
	}
	sb := ScreenBrightness{}
	err = sb.getScreenBrightness()
	if err != nil {
		return fmt.Errorf("couldn't get screen brightness: %w", err)
	}
	so := ScreenOn{}
	err = so.getScreenOn()
	if err != nil {
		return fmt.Errorf("couldn't get screen on: %w", err)
	}
	ch <- c.metric[0].mustNewConstMetric(float64(battery.Level))
	ch <- c.metric[1].mustNewConstMetric(float64(battery.Temperature))
	ch <- c.metric[2].mustNewConstMetric(float64(battery.Status))
	ch <- c.metric[3].mustNewConstMetric(float64(battery.Health))
	ch <- c.metric[4].mustNewConstMetric(float64(battery.Voltage))
	if battery.ACPowered {
		ch <- c.metric[5].mustNewConstMetric(float64(1))
	} else {
		ch <- c.metric[5].mustNewConstMetric(float64(0))
	}
	ch <- c.metric[6].mustNewConstMetric(float64(sb.Number))
	// xm_device_screen_on 0: full on; 1..3: power saving modes; 4:full off
	ch <- c.metric[7].mustNewConstMetric(float64(so.mScreenOn))

	return err
}
