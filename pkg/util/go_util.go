package util

import (
	"fmt"
	"github.com/golang/glog"
	"strconv"
	"strings"
	"time"
)

//compare two version strings, for example:
// v1: "1.4.9",  v2: "1.5", then return -1
// v1: "1.5.0", v2: "1.5", then return 0
func CompareVersion(version1, version2 string) int {
	a1 := strings.Split(version1, ".")
	a2 := strings.Split(version2, ".")

	l1 := len(a1)
	l2 := len(a2)
	mlen := l1
	if mlen < l2 {
		mlen = l2
	}

	for i := 0; i < mlen; i++ {
		b1 := 0
		if i < l1 {
			if tmp, err := strconv.Atoi(a1[i]); err == nil {
				b1 = tmp
			}
		}

		b2 := 0
		if i < l2 {
			if tmp, err := strconv.Atoi(a2[i]); err == nil {
				b2 = tmp
			}
		}

		if b1 != b2 {
			return b1 - b2
		}
	}

	return 0
}

//retry to execute a function with a timeout
func RetryDuring(attempts int, timeout time.Duration, sleep time.Duration, myfunc func() error) error {
	t0 := time.Now()

	var err error
	for i := 0; ; i++ {
		if err = myfunc(); err == nil {
			glog.V(4).Infof("[retry-%d/%d] success", i+1, attempts)
			return nil
		}

		glog.V(4).Infof("[retry-%d/%d] Warning %v", i+1, attempts, err)
		if i >= (attempts - 1) {
			break
		}

		if timeout > 0 {
			if delta := time.Now().Sub(t0); delta > timeout {
				err = fmt.Errorf("failed after %d attepmts (during %v) last error: %v", i+1, delta, err)
				glog.Error(err)
				return err
			}
		}

		if sleep > 0 {
			time.Sleep(sleep)
		}
	}

	err = fmt.Errorf("failed after %d attepmts, last error: %v", attempts, err)
	glog.Error(err)
	return err
}

//retry to execute a function with a timeout
func RetrySimple(attempts int, timeout, sleep time.Duration, myfunc func() (bool, error)) error {
	t0 := time.Now()

	var err error
	for i := 0; ; i++ {
		retry := false
		if retry, err = myfunc(); !retry {
			return err
		}

		glog.V(4).Infof("[retry-%d/%d] Warning %v", i+1, attempts, err)
		if i >= (attempts - 1) {
			break
		}

		if timeout > 0 {
			if delta := time.Now().Sub(t0); delta > timeout {
				err = fmt.Errorf("failed after %d attepmts (during %v) last error: %v", i+1, delta, err)
				glog.Error(err)
				return err
			}
		}

		if sleep > 0 {
			time.Sleep(sleep)
		}
	}

	err = fmt.Errorf("failed after %d attepmts, last error: %v", attempts, err)
	glog.Error(err)
	return err
}
