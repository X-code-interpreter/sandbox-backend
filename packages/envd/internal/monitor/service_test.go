package monitor

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

func TestEntries(t *testing.T) {
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("create zap logger failed: %v", err)
	}
	m := NewMonitor(logger.Sugar())
	ch := make(chan prometheus.Metric)
	go func() {
		for m := range ch {
			t.Logf("recv metric %s", m.Desc())
		}
	}()
	for i := 0; i < 10; i++ {
		time.Sleep(time.Second)
		m.Collect(ch)
	}
	close(ch)
}
