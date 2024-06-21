package monitor

import (
	"fmt"
	"reflect"

	"github.com/prometheus/client_golang/prometheus"
)

type MemMetric struct {
	Available    prometheus.Gauge
	Free         prometheus.Gauge
	Cached       prometheus.Gauge
	Used         prometheus.Gauge
	ActiveFile   prometheus.Gauge
	InActiveFile prometheus.Gauge
	ActiveAnon   prometheus.Gauge
	InActiveAnon prometheus.Gauge
	SwapFree     prometheus.Gauge
}

type NetMetric struct {
	BytesSend  *prometheus.GaugeVec
	BytesRecv  *prometheus.GaugeVec
	PacketSend *prometheus.GaugeVec
	PacketRecv *prometheus.GaugeVec
}

func NewMemMetric() MemMetric {
	return MemMetric{
		Available: prometheus.NewGauge(
			prometheus.GaugeOpts{Subsystem: "mem", Name: "avaiable"},
		),
		Free: prometheus.NewGauge(
			prometheus.GaugeOpts{Subsystem: "mem", Name: "free"},
		),
		Cached: prometheus.NewGauge(
			prometheus.GaugeOpts{Subsystem: "mem", Name: "cached"},
		),
		Used: prometheus.NewGauge(
			prometheus.GaugeOpts{Subsystem: "mem", Name: "used"},
		),
		ActiveFile: prometheus.NewGauge(
			prometheus.GaugeOpts{Subsystem: "mem", Name: "activeFile"},
		),
		InActiveFile: prometheus.NewGauge(
			prometheus.GaugeOpts{Subsystem: "mem", Name: "inactiveFile"},
		),
		ActiveAnon: prometheus.NewGauge(
			prometheus.GaugeOpts{Subsystem: "mem", Name: "activeAnon"},
		),
		InActiveAnon: prometheus.NewGauge(
			prometheus.GaugeOpts{Subsystem: "mem", Name: "inactiveAnon"},
		),
		SwapFree: prometheus.NewGauge(
			prometheus.GaugeOpts{Subsystem: "mem", Name: "swapFree"},
		),
	}
}

func NewNetMetric() NetMetric {
	return NetMetric{
		BytesSend: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{Subsystem: "net", Name: "bytesSend"},
			[]string{"interface"},
		),
		BytesRecv: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{Subsystem: "net", Name: "bytesRecv"},
			[]string{"interface"},
		),
		PacketSend: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{Subsystem: "net", Name: "packetSend"},
			[]string{"interface"},
		),
		PacketRecv: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{Subsystem: "net", Name: "packetRecv"},
			[]string{"interface"},
		),
	}
}

type MetricSet struct {
	cpu *prometheus.GaugeVec
	net NetMetric
	mem MemMetric
}

func NewMetricSet() *MetricSet {
	return &MetricSet{
		cpu: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Subsystem: "cpu",
			Name:      "percent",
		}, []string{"cpuid"}),
		mem: NewMemMetric(),
		net: NewNetMetric(),
	}
}

func (m *MetricSet) describe(ch chan<- *prometheus.Desc) {
	m.cpu.Describe(ch)

	v := reflect.ValueOf(m.mem)
	for i := 0; i < v.NumField(); i++ {
		metric, ok := v.Field(i).Interface().(prometheus.Collector)
		if !ok {
			ch <- prometheus.NewInvalidDesc(fmt.Errorf("reflect for field in MemMetric failed: %s", v.Type().Field(i).Name))
			return
		}
		metric.Describe(ch)
	}

	v = reflect.ValueOf(m.net)
	for i := 0; i < v.NumField(); i++ {
		metric, ok := v.Field(i).Interface().(prometheus.Collector)
		if !ok {
			ch <- prometheus.NewInvalidDesc(fmt.Errorf("reflect for field in NetMetric failed: %s", v.Type().Field(i).Name))
			return
		}
		metric.Describe(ch)
	}
}
