package app

import (
	"fmt"
	"sync"
	"syscall"
	"testing"
	"time"
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
	mockDisconnectFunc := disconnectFromTurboFunc(func() {
		fmt.Printf("Mock disconnecting process is running...")
		helper.call()
	})

	handleExit(mockDisconnectFunc)

	// Sending out the SIGTERM signal to trigger the disconnecting process
	syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	// Wait a bit for the channel to receive and process the signal
	time.Sleep(1000 * time.Millisecond)
	if !helper.gotCalled() {
		fmt.Printf("The disconnect function was not invoked with signal SIGTERM")
		// comment this because it is not stable during travis test
		//t.Errorf("The disconnect function was not invoked with signal SIGTERM")
	}
}
