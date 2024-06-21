package monitor

import (
	"strconv"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"go.uber.org/zap"
)

type SystemMonitor struct {
	logger *zap.SugaredLogger
	mu     sync.RWMutex
	metric *MetricSet
}

func NewService(logger *zap.SugaredLogger) *SystemMonitor {
	return NewMonitor(logger)
}

func NewMonitor(logger *zap.SugaredLogger) *SystemMonitor {
	s := &SystemMonitor{
		logger: logger,
		metric: NewMetricSet(),
	}
	return s
}

func (s *SystemMonitor) Describe(ch chan<- *prometheus.Desc) {
	s.metric.describe(ch)
}

func (s *SystemMonitor) Collect(ch chan<- prometheus.Metric) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cpuUsage, err := cpu.Percent(0, true)
	if err != nil {
		s.logger.Errorw("collect cpu metric failed",
			"err", err,
		)
		return
	}
	for cpuIdx, percent := range cpuUsage {
		s.metric.cpu.WithLabelValues("cpu-" + strconv.Itoa(cpuIdx)).Set(percent)
	}
	s.metric.cpu.Collect(ch)

	vmEx, err := mem.NewExLinux().VirtualMemory()
	if err != nil {
		s.logger.Errorw("collect ExLinux memory metric failed",
			"err", err,
		)
		return
	}
	s.metric.mem.ActiveFile.Set(float64(vmEx.ActiveFile))
	s.metric.mem.ActiveFile.Collect(ch)

	s.metric.mem.InActiveFile.Set(float64(vmEx.InactiveFile))
	s.metric.mem.InActiveFile.Collect(ch)

	s.metric.mem.ActiveAnon.Set(float64(vmEx.ActiveAnon))
	s.metric.mem.ActiveAnon.Collect(ch)

	s.metric.mem.InActiveAnon.Set(float64(vmEx.InactiveAnon))
	s.metric.mem.InActiveAnon.Collect(ch)

	memUsage, err := mem.VirtualMemory()
	if err != nil {
		s.logger.Errorw("collect memory metric failed",
			"err", err,
		)
		return
	}
	s.metric.mem.Available.Set(float64(memUsage.Available))
	s.metric.mem.Available.Collect(ch)

	s.metric.mem.Free.Set(float64(memUsage.Free))
	s.metric.mem.Free.Collect(ch)

	s.metric.mem.Cached.Set(float64(memUsage.Cached))
	s.metric.mem.Cached.Collect(ch)

	s.metric.mem.Used.Set(float64(memUsage.Used))
	s.metric.mem.Used.Collect(ch)

	s.metric.mem.SwapFree.Set(float64(memUsage.SwapFree))
	s.metric.mem.SwapFree.Collect(ch)

	netCounters, err := net.IOCounters(true)
	if err != nil {
		s.logger.Errorw("collect network metric failed",
			"err", err,
		)
		return
	}
	for _, c := range netCounters {
		s.metric.net.BytesSend.WithLabelValues(c.Name).Set(float64(c.BytesSent))
		s.metric.net.BytesRecv.WithLabelValues(c.Name).Set(float64(c.BytesRecv))
		s.metric.net.PacketSend.WithLabelValues(c.Name).Set(float64(c.PacketsSent))
		s.metric.net.PacketRecv.WithLabelValues(c.Name).Set(float64(c.PacketsRecv))
	}
	s.metric.net.BytesSend.Collect(ch)
	s.metric.net.BytesRecv.Collect(ch)
	s.metric.net.PacketSend.Collect(ch)
	s.metric.net.PacketRecv.Collect(ch)
}
