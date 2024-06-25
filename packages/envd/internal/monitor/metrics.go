package monitor

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

const (
	namespace = "fcvm"
)

type MemMetric struct {
	mu   sync.Mutex
	desc map[string]*prometheus.Desc
}

func (m *MemMetric) getDesc(statName string) *prometheus.Desc {
	m.mu.Lock()
	defer m.mu.Unlock()
	desc, ok := m.desc[statName]
	if !ok {
		desc = prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "mem", statName),
			fmt.Sprintf("memory stats: %s", statName),
			nil,
			nil,
		)
		m.desc[statName] = desc
	}
	return desc
}

func (m *MemMetric) collect(ch chan<- prometheus.Metric) error {
	stats := make(map[string]uint64)
	vmEx, err := mem.NewExLinux().VirtualMemory()
	if err != nil {
		return fmt.Errorf("collect exlinux memory failed: %w", err)
	}
	memUsage, err := mem.VirtualMemory()
	if err != nil {
		return fmt.Errorf("collect memory metric failed: %w", err)
	}

	stats["active_file"] = vmEx.ActiveFile
	stats["inactive_file"] = vmEx.InactiveFile
	stats["active_anon"] = vmEx.ActiveAnon
	stats["inactive_anon"] = vmEx.InactiveAnon

	stats["available"] = memUsage.Available
	stats["free"] = memUsage.Free
	stats["cached"] = memUsage.Cached
	stats["used"] = memUsage.Used
	stats["swapfree"] = memUsage.SwapFree

	for key, val := range stats {
		desc := m.getDesc(key)
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(val))
	}
	return nil
}

type NetMetric struct {
	// interface-name is the key
	mu   sync.Mutex
	desc map[string]*prometheus.Desc
}

func (m *NetMetric) getDesc(statName string) *prometheus.Desc {
	m.mu.Lock()
	defer m.mu.Unlock()
	desc, ok := m.desc[statName]
	if !ok {
		desc = prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "net", statName),
			fmt.Sprintf("network counter: %s", statName),
			[]string{"interface"},
			nil,
		)
		m.desc[statName] = desc
	}
	return desc
}

func (m *NetMetric) collect(ch chan<- prometheus.Metric) error {
	netCounters, err := net.IOCounters(true)
	if err != nil {
		return fmt.Errorf("collect network metric failed: %w", err)
	}
	for _, c := range netCounters {
		stats := make(map[string]uint64)
		stats["tx_bytes"] = c.BytesSent
		stats["rx_bytes"] = c.BytesRecv
		stats["tx_pkt"] = c.PacketsSent
		stats["rx_pkt"] = c.PacketsRecv
		for k, v := range stats {
			desc := m.getDesc(k)
			ch <- prometheus.MustNewConstMetric(desc, prometheus.CounterValue, float64(v), c.Name)
		}
	}
	return nil
}

type CpuMetric struct {
	metric *prometheus.GaugeVec
}

func NewCpuMetric() CpuMetric {
	return CpuMetric{
		metric: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Subsystem: "cpu",
			Name:      "percent",
		}, []string{"cpuid"}),
	}
}

func (m CpuMetric) collect(ch chan<- prometheus.Metric) error {
	cpuUsage, err := cpu.Percent(0, true)
	if err != nil {
		return fmt.Errorf("collect cpu metric failed: %w", err)
	}
	for cpuIdx, percent := range cpuUsage {
		m.metric.WithLabelValues(strconv.Itoa(cpuIdx)).Set(percent)
	}
	m.metric.Collect(ch)
	return nil
}

type MetricSet struct {
	cpu CpuMetric
	net *NetMetric
	mem *MemMetric
}

func NewMetricSet() MetricSet {
	return MetricSet{
		cpu: NewCpuMetric(),
		mem: &MemMetric{
			desc: make(map[string]*prometheus.Desc),
		},
		net: &NetMetric{
			desc: make(map[string]*prometheus.Desc),
		},
	}
}
