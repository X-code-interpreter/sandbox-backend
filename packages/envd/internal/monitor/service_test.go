package monitor

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestEntries(t *testing.T) {
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("create zap logger failed: %v", err)
	}
	m := NewMonitor(logger.Sugar(), 20, 600*time.Millisecond)
	for i := 0; i < 10; i++ {
		time.Sleep(time.Second)
		entries := m.copyMetrics()
		t.Logf("after sleep, the entry num is %v", len(entries))
		t.Logf("recent net %+v", entries[len(entries)-1].NetUsage["lo"])
		var prevEntry MetricEntry
		for idx, e := range entries {
			if idx == 0 {
				prevEntry = e
				continue
			}
			if e.Timestamp.Before(prevEntry.Timestamp) || e.Timestamp.Equal(prevEntry.Timestamp) {
				t.Fatalf("find weird results: prev %v current %v", prevEntry.Timestamp, e.Timestamp)
			}
			prevEntry = e
		}
	}
}
