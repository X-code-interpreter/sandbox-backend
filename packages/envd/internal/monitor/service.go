package monitor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"go.uber.org/zap"
)

const MaxEntryNum = 5 * 60 * 60

type MemMetric struct {
	Available uint64 `json:"avaiable"`
	Free      uint64 `json:"free"`
	Cached    uint64 `json:"cached"`
	mem.ExVirtualMemory
}

type NetMetric struct {
	TxBw           float64 `json:"txBandwidth"`
	RxBw           float64 `json:"rxBandwidth"`
	PacketSendRate float64 `json:"packetSendRate"` // number of packets sent
	PacketRecvRate float64 `json:"packetRecvRate"` // number of packets received
}

type MetricEntry struct {
	CpuUsage  []float64            `json:"cpu"`
	MemUsage  MemMetric            `json:"mem"`
	NetUsage  map[string]NetMetric `json:"net"`
	Timestamp time.Time            `json:"timestamp"`
}

type SystemMonitor struct {
	logger   *zap.SugaredLogger
	mu       sync.RWMutex
	entries  []MetricEntry
	next     int
	full     bool
	capacity int
	interval time.Duration
}

func NewService(logger *zap.SugaredLogger) *SystemMonitor {
	return NewMonitor(logger, MaxEntryNum, time.Second)
}

func NewMonitor(logger *zap.SugaredLogger, capacity int, interval time.Duration) *SystemMonitor {
	s := &SystemMonitor{
		logger:   logger,
		entries:  make([]MetricEntry, capacity),
		next:     0,
		full:     false,
		capacity: capacity,
		interval: interval,
	}
	go s.collect()
	return s
}

func (s *SystemMonitor) GetMetric(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	entries := s.copyMetrics()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(entries); err != nil {
		http.Error(w, "encode metric entries to json failed", http.StatusInternalServerError)
		s.logger.Errorw("encode metric entries to json failed",
			"err", err,
		)
	}
}

func calculateNetBw(curr []net.IOCountersStat, prev []net.IOCountersStat, dur time.Duration) (map[string]NetMetric, error) {
	res := make(map[string]NetMetric)
	interval := dur.Seconds()
	for _, currStat := range curr {
		var prevId int
		for prevId = 0; prevId < len(prev); prevId++ {
			if prev[prevId].Name == currStat.Name {
				break
			}
		}
		if prevId == len(prev) {
			return nil, fmt.Errorf("not compatiable net io counter for interface %s", currStat.Name)
		}
		prevStat := prev[prevId]

		// calculate diff
		res[currStat.Name] = NetMetric{
			TxBw:           float64(currStat.BytesSent-prevStat.BytesSent) / interval,
			RxBw:           float64(currStat.BytesRecv-prevStat.BytesRecv) / interval,
			PacketSendRate: float64(currStat.PacketsSent-prevStat.PacketsSent) / interval,
			PacketRecvRate: float64(currStat.PacketsRecv-prevStat.PacketsRecv) / interval,
		}
	}
	return res, nil
}

func (s *SystemMonitor) collect() {
	var (
		currTs          time.Time
		prevTs          time.Time
		prevNetCounters []net.IOCountersStat
		err             error
	)
	// retry until succeed!
	for {
		prevNetCounters, err = net.IOCounters(true)
		if err != nil {
			s.logger.Errorw("collect net counters failed",
				"err", err,
			)
			time.Sleep(time.Second)
		} else {
			break
		}
	}
	prevTs = time.Now()

	for {
		time.Sleep(s.interval)
		cpuUsage, err := cpu.Percent(0, true)
		if err != nil {
			s.logger.Errorw("collect cpu metric failed",
				"err", err,
			)
			continue
		}
		vmEx, err := mem.NewExLinux().VirtualMemory()
		if err != nil {
			s.logger.Errorw("collect ExLinux memory metric failed",
				"err", err,
			)
			continue
		}
		memUsage, err := mem.VirtualMemory()
		if err != nil {
			s.logger.Errorw("collect memory metric failed",
				"err", err,
			)
			continue
		}
		netCounters, err := net.IOCounters(true)
		if err != nil {
			s.logger.Errorw("collect network metric failed",
				"err", err,
			)
			continue
		}
		currTs = time.Now()
		netUsage, err := calculateNetBw(netCounters, prevNetCounters, currTs.Sub(prevTs))
		prevNetCounters = netCounters
		prevTs = currTs
		if err != nil {
			s.logger.Errorw("calculate bw failed",
				"err", err,
			)
			continue
		}

		// push to entries
		s.mu.Lock()
		entry := &s.entries[s.next]
		*entry = MetricEntry{
			CpuUsage: cpuUsage,
			MemUsage: MemMetric{
				Available:       memUsage.Available,
				Free:            memUsage.Free,
				Cached:          memUsage.Cached,
				ExVirtualMemory: *vmEx,
			},
			NetUsage:  netUsage,
			Timestamp: currTs,
		}
		s.next = (s.next + 1) % s.capacity
		if s.next == 0 {
			s.full = true
		}
		s.mu.Unlock()
	}
}

func (s *SystemMonitor) copyMetrics() []MetricEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var entries []MetricEntry
	if s.full {
		entries = make([]MetricEntry, s.capacity)
		// buffer is full, so the oldest one is at s.next
		// newest one is at s.next - 1
		num := copy(entries, s.entries[s.next:])
		copy(entries[num:], s.entries[:s.next])
	} else {
		// buffer is not full, so the oldest one is at 0
		// the newest one is at s.next - 1
		entries = make([]MetricEntry, s.next)
		copy(entries, s.entries[:s.next])
	}
	return entries
}
