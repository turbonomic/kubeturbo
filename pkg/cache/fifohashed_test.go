package cache

import (
	"fmt"
	"testing"
)

func myKeyFunc(obj interface{}) (string, error) {
	s, ok := obj.(string)
	if !ok {
		return "", fmt.Errorf("no string")
	}
	return s, nil
}

func TestHashedFIFO_GetByKey(t *testing.T) {
	fifo := NewHashedFIFO(myKeyFunc)
	items := []string{"hello", "world"}

	for _, item := range items {
		fifo.Add(item)
	}

	for _, item := range items {
		_, flag, err := fifo.GetByKey(item)
		if err != nil || !flag {
			t.Error("test failed.")
		}
	}
}
