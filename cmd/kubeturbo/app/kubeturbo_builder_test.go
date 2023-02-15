package app

import (
	"fmt"
	"sync"
	"syscall"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	kubeturbo "github.com/turbonomic/kubeturbo/pkg"
)

type helper struct {
	funcGotCalled bool
}

var mux sync.Mutex

func (h *helper) call() {
	mux.Lock()
	h.funcGotCalled = true
	mux.Unlock()
}

func (h *helper) gotCalled() bool {
	mux.Lock()
	defer mux.Unlock()
	return h.funcGotCalled
}

func Test_handleExit(t *testing.T) {
	helper := helper{false}
	mockDisconnectFunc := cleanUp(func() {
		fmt.Printf("Mock disconnecting process is running...")
		helper.call()
	})

	wg := &sync.WaitGroup{}
	handleExit(wg, mockDisconnectFunc)

	// Sending out the SIGTERM signal to trigger the disconnecting process
	syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	// Wait for all goroutines to finish
	wg.Wait()
	if !helper.gotCalled() {
		fmt.Printf("The disconnect function was not invoked with signal SIGTERM")
		t.Errorf("The disconnect function was not invoked with signal SIGTERM")
	}
}

func TestOptions(t *testing.T) {
	vmtConfig := kubeturbo.NewVMTConfig2()
	vmtConfig.
		WithVMIsBase(true).
		WithDiscoveryInterval(1).
		WithValidationTimeout(2).
		WithValidationWorkers(3)
	assert.True(t, vmtConfig.VMIsBase)
	assert.Equal(t, vmtConfig.DiscoveryIntervalSec, 1)
	assert.Equal(t, vmtConfig.ValidationTimeoutSec, 2)
	assert.Equal(t, vmtConfig.ValidationWorkers, 3)
}

func TestOptionsSet(t *testing.T) {
	s := VMTServer{
		Port:       100,
		Address:    "127.0.0.1",
		VMPriority: 10,
		VMIsBase:   true,
	}
	s.AddFlags(pflag.CommandLine)
}
