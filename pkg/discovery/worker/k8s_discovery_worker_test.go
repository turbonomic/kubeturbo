package worker

import (
	"testing"
	"time"
)

func TestCalTimeOut(t *testing.T) {
	type Pair struct {
		nodeNum int
		timeout time.Duration
	}

	inputs := []*Pair{
		&Pair{
			nodeNum: 0,
			timeout: defaultMonitoringWorkerTimeout,
		},
		&Pair{
			nodeNum: 22,
			timeout: defaultMonitoringWorkerTimeout + time.Second*22,
		},
		&Pair{
			nodeNum: 500,
			timeout: defaultMonitoringWorkerTimeout + time.Second*500,
		},
		&Pair{
			nodeNum: 1500,
			timeout: defaultMaxTimeout,
		},
	}

	for _, input := range inputs {
		result := calcTimeOut(input.nodeNum)
		if result != input.timeout {
			t.Errorf("tiemout error: %v Vs. %v", result, input.timeout)
		}
	}
}
