/*
Based upon https://github.com/kubernetes/kubernetes/blob/master/pkg/scheduler/framework/parallelize/parallelism.go
*/
package parallelizer

import (
	"context"
	"math"
	"runtime"

	"k8s.io/client-go/util/workqueue"
	"k8s.io/component-base/metrics"
)

const (
	SchedulerSubsystem = "scheduler"
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

func NewParallelizer() Parallelizer {
	return Parallelizer{parallelism: runtime.NumCPU()}
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
