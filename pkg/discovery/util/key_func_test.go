package util

import (
	"testing"
)

func TestParseContainerId(t *testing.T) {
	type mytriple struct {
		input string
		podId string
		index int
	}

	tests := []*mytriple{
		&mytriple{
			input: "a-1",
			podId: "a",
			index: 1,
		},
		&mytriple{
			input: "1466089904-b2333a79-2901-11e7-a3d7-00505680effd-0",
			podId: "1466089904-b2333a79-2901-11e7-a3d7-00505680effd",
			index: 0,
		},
		&mytriple{
			input: "1466089904-b2333a79-2901-11e7-a3d7-00505680effd-1",
			podId: "1466089904-b2333a79-2901-11e7-a3d7-00505680effd",
			index: 1,
		},
		&mytriple{
			input: "1466089904-b2333a79-2901-11e7-a3d7-00505680effd-10",
			podId: "1466089904-b2333a79-2901-11e7-a3d7-00505680effd",
			index: 10,
		},
		&mytriple{
			input: "1466089904-b2333a79-2901-11e7-a3d7-00505680effd-100",
			podId: "1466089904-b2333a79-2901-11e7-a3d7-00505680effd",
			index: 100,
		},
	}

	for _, test := range tests {
		podId, index, err := ParseContainerId(test.input)
		if err != nil {
			t.Error(err)
		}

		if podId != test.podId || index != test.index {
			t.Errorf("mismatch: [%s, %d] Vs. [%s, %d]", podId, index, test.podId, test.index)
		}
	}
}

func TestParseContainerID2(t *testing.T) {
	//1. good
	input := "1466089904-b2333a79-2901-11e7-a3d7-00505680effd-0"
	expectPodId := "1466089904-b2333a79-2901-11e7-a3d7-00505680effd"
	expectIndex := 0
	podId, index, err := ParseContainerId(input)
	if err != nil {
		t.Error(err)
	}

	if podId != expectPodId || index != expectIndex {
		t.Errorf("mismatch: [%s, %d] Vs. [%s, %d]", podId, index, expectPodId, expectIndex)
	}

	//2. bad
	badtests := []string{
		"-0",
		"1466089904-b2333a79-2901-11e7-a3d7-00505680effd",
		"1466089904-b2333a79-2901-11e7-a3d7-",
		"adfdfadf",
		"123",
		"----",
	}
	for _, bad := range badtests {
		_, _, err = ParseContainerId(bad)
		if err == nil {
			t.Errorf("should not parse a invalid containerId[%s]", bad)
		}
	}
}
