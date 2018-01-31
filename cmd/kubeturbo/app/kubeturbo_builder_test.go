package app

import (
	"fmt"
	"syscall"
	"testing"
	"time"
)

func Test_handleExit(t *testing.T) {
	disconnectFuncGotCalled := false
	mockDisconnectFunc := disconnectFromTurboFunc(func() {
		fmt.Printf("Mock disconnecting process is running...")
		disconnectFuncGotCalled = true
	})

	handleExit(mockDisconnectFunc)
	disconnectFuncGotCalled = false

	// Sending out the SIGTERM signal to trigger the disconnecting process
	syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	// Wait a bit for the channel to receive and process the signal
	time.Sleep(100 * time.Millisecond)
	if !disconnectFuncGotCalled {
		t.Errorf("The disconnect function was not invoked with signal SIGTERM")
	}
}
