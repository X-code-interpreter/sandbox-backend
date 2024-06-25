package monitor

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

type SystemMonitor struct {
	MetricSet
	logger *zap.SugaredLogger
}

func NewService(logger *zap.SugaredLogger) *SystemMonitor {
	return NewMonitor(logger)
}

func NewMonitor(logger *zap.SugaredLogger) *SystemMonitor {
	s := &SystemMonitor{
		MetricSet: NewMetricSet(),
		logger:    logger,
	}
	return s
}

func (s *SystemMonitor) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(s, ch)
}

func (s *SystemMonitor) Collect(ch chan<- prometheus.Metric) {
	if err := s.cpu.collect(ch); err != nil {
		s.logger.Errorw("error when collect", "err", err)
	}
	if err := s.mem.collect(ch); err != nil {
		s.logger.Errorw("error when collect", "err", err)
	}
	if err := s.net.collect(ch); err != nil {
		s.logger.Errorw("error when collect", "err", err)
	}
}
