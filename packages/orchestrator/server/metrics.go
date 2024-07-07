package server

import (
	"context"
	"fmt"
	"time"

	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/constants"
	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/sandbox"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const (
	Second = 1000 //ms
)

var (
	deactiveMemBoundaries = make([]float64, 0, 64)
	deactiveDurBoundaries = make([]float64, 0, 64)
)

func init() {
	// every 64MB is a bucket
	for bound := 64; bound < 2*1024; bound += 64 {
		deactiveMemBoundaries = append(deactiveMemBoundaries, float64(bound))
	}

	// 10ms - 100ms: each 10ms has one bucket
	// 100ms - 1s: each 50ms has one bucket
	// 1s - 10s: each 500 ms has one bucket
	for bound := 10; bound < 100; bound += 10 {
		deactiveDurBoundaries = append(deactiveDurBoundaries, float64(bound))
	}
	for bound := 100; bound < Second; bound += 100 {
		deactiveDurBoundaries = append(deactiveDurBoundaries, float64(bound))
	}
	for bound := Second; bound < 10*Second; bound += 500 {
		deactiveDurBoundaries = append(deactiveDurBoundaries, float64(bound))
	}
}

type serverMetric struct {
	total metric.Int64UpDownCounter
	// The time spent on deactiving a sandbox
	deactiveDur metric.Float64Histogram
	// The memory save on deactiving a sandbox
	deactiveMem metric.Float64Histogram
}

func newServerMetric() (*serverMetric, error) {
	meter := otel.Meter(constants.ServiceName)
	total, err := meter.Int64UpDownCounter(
		"sandbox.total_counter",
		metric.WithDescription("Total number of valid sandbox (including those being killed)"),
	)
	if err != nil {
		return nil, fmt.Errorf("create metric `total` failed: %w", err)
	}

	deactiveDur, err := meter.Float64Histogram(
		"deactive.duration",
		metric.WithDescription("The duration of deactiving a sandbox (in milliseconds)"),
		metric.WithExplicitBucketBoundaries(deactiveDurBoundaries...),
	)
	if err != nil {
		return nil, fmt.Errorf("create metric `deactive` failed: %w", err)
	}

	deactiveMem, err := meter.Float64Histogram(
		"deactive.memory",
		metric.WithDescription("The memory saving of deactiving a sandbox (in MB)"),
		metric.WithExplicitBucketBoundaries(deactiveMemBoundaries...),
	)
	if err != nil {
		return nil, fmt.Errorf("create metric `deactive` failed: %w", err)
	}
	return &serverMetric{
		total:       total,
		deactiveDur: deactiveDur,
		deactiveMem: deactiveMem,
	}, nil
}

func (m *serverMetric) AddSandbox(ctx context.Context, sbx *sandbox.Sandbox) {
	m.total.Add(ctx, 1)
}

func (m *serverMetric) DelSandbox(ctx context.Context, sbx *sandbox.Sandbox) {
	m.total.Add(ctx, -1)
}

// Finally it will record milliseconds
func (m *serverMetric) RecordDeactiveDuration(ctx context.Context, sbx *sandbox.Sandbox, dur time.Duration) {
	ms := float64(dur.Nanoseconds()) / 1e6
	m.deactiveDur.Record(ctx, ms)
}

// the amount is the value of bytes
func (m *serverMetric) RecordDeactiveMem(ctx context.Context, sbx *sandbox.Sandbox, amount int64) {
	amount_in_mb := float64(amount) / (1024 * 1024)
	m.deactiveMem.Record(ctx, amount_in_mb)
}
