package kubeclient

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLinuxAmd64NodeCpuFrequencyGetter_ParseCpuFrequency(t *testing.T) {
	jobLog := "cpu MHz\t\t: 2199.999"
	getter := &LinuxAmd64NodeCpuFrequencyGetter{
		NodeCpuFrequencyGetter: *NewNodeCpuFrequencyGetter(nil, "", ""),
	}
	cpuFreq, err := getter.ParseCpuFrequency(jobLog)
	assert.Nil(t, err)
	assert.Equal(t, 2199.999, cpuFreq)
}

func TestLinuxPpc64leNodeCpuFrequencyGetter_ParseCpuFrequency(t *testing.T) {
	jobLog := "clock\t\t: 2500.000000MHz"
	getter := &LinuxPpc64leNodeCpuFrequencyGetter{
		NodeCpuFrequencyGetter: *NewNodeCpuFrequencyGetter(nil, "", ""),
	}
	cpuFreq, err := getter.ParseCpuFrequency(jobLog)
	assert.Nil(t, err)
	assert.Equal(t, 2500.0, cpuFreq)
}

func TestBackoffFailures(t *testing.T) {
	type failedTimes int
	type testCase struct {
		checkAfter    time.Duration // incremental sleep for each next check
		shouldBackoff bool
	}
	testCaseMap := map[failedTimes][]testCase{
		0: { // No backoff anytime
			{checkAfter: 10 * time.Millisecond, shouldBackoff: false},
			{checkAfter: 100 * time.Millisecond, shouldBackoff: false},
			{checkAfter: 200 * time.Millisecond, shouldBackoff: false},
		},
		1: { // Backoff till 2 exponent 1 times 100ms = 200ms
			{checkAfter: 0 * time.Millisecond, shouldBackoff: true},
			{checkAfter: 100 * time.Millisecond, shouldBackoff: true},
			{checkAfter: 200 * time.Millisecond, shouldBackoff: false},
		},
		2: { // Backoff till 2 exponent 2 times 100ms = 400ms
			{checkAfter: 0 * time.Millisecond, shouldBackoff: true},
			{checkAfter: 100 * time.Millisecond, shouldBackoff: true},
			{checkAfter: 200 * time.Millisecond, shouldBackoff: true},
			{checkAfter: 200 * time.Millisecond, shouldBackoff: false},
		},
		4: { // Backoff till 2 exponent 4 times 100ms = 1600ms
			{checkAfter: 0 * time.Millisecond, shouldBackoff: true},     //t0
			{checkAfter: 400 * time.Millisecond, shouldBackoff: true},   //t0+400
			{checkAfter: 800 * time.Millisecond, shouldBackoff: true},   //t0+400+400
			{checkAfter: 1800 * time.Millisecond, shouldBackoff: false}, //t0+400+400+1000
		},
	}

	checkDuration := 100 * time.Millisecond
	var wg sync.WaitGroup
	for failures, tests := range testCaseMap {
		wg.Add(1)
		go func(failures failedTimes, tests []testCase) {
			backoff := newBackoff(checkDuration)
			for i := failures; i > 0; i-- {
				backoff.setFailure()
			}
			for index, test := range tests {
				time.Sleep(test.checkAfter)
				b := backoff.backoff()
				assert.Equal(t, test.shouldBackoff, b,
					"Failed at index: %d for failure times: %d check at %v\n", index, failures, time.Now())
			}
			wg.Done()
		}(failures, tests)
	}
	wg.Wait()
}
