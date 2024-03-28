/*
Based upon https://github.com/kubernetes/kubernetes/blob/master/pkg/scheduler/framework/parallelize/parallelism.go
*/
package parallelizer

import (
	"context"
	"math"

	"k8s.io/client-go/util/workqueue"
	"k8s.io/component-base/metrics"
)

const (
	DefaultParallelism int = 10
	SchedulerSubsystem     = "scheduler"
)

var (
	Goroutines = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      SchedulerSubsystem,
			Name:           "goroutines",
			Help:           "Number of running goroutines split by the work they do such as binding.",
			StabilityLevel: metrics.ALPHA,
		}, []string{"operation"})
)

type Parallelizer struct {
	parallelism int
}

func NewParallelizer(p int) Parallelizer {
	return Parallelizer{parallelism: p}
}

func chunkSizeFor(n, parallelism int) int {
	s := int(math.Sqrt(float64(n)))
	if r := n/parallelism + 1; s > r {
		s = r
	} else if s < 1 {
		s = 1
	}
	return s
}

func (p Parallelizer) Until(ctx context.Context, pieces int, doWorkPiece workqueue.DoWorkPieceFunc, operation string) {
	withMetrics := func(piece int) {
		Goroutines.WithLabelValues(operation).Inc()
		defer Goroutines.WithLabelValues(operation).Dec()
		doWorkPiece(piece)
	}
	workqueue.ParallelizeUntil(ctx, p.parallelism, pieces, withMetrics, workqueue.WithChunkSize(chunkSizeFor(pieces, p.parallelism)))
}
