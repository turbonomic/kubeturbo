package kubeclient

import (
	"github.com/stretchr/testify/assert"
	"testing"
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
