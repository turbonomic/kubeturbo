package worker

import (
	"fmt"
	"math"
	"testing"
)

func TestDispatcher_DispatchMimic(t *testing.T) {

	nodeNum := 5
	workerCount := 4

	receiveNum := 0
	queue := make(chan int)

	seg := int(math.Ceil(float64(nodeNum) / (float64(workerCount))))
	fmt.Printf("seg = %d\n", seg)

	go func() {
		defer close(queue)

		assignedNum := 0
		nodes := make([]int, nodeNum)
		for i := range nodes {
			nodes[i] = i
		}

		for assignedNum+seg <= nodeNum {
			task := nodes[assignedNum : assignedNum+seg]

			for _, t := range task {
				queue <- t
			}
			assignedNum += seg
		}

		//if assignedNum < nodeNum - 1 {
		if assignedNum < nodeNum {
			task := nodes[assignedNum:]
			for _, t := range task {
				queue <- t
			}
		}
	}()

	a := 0
	for t := range queue {
		receiveNum += 1
		a += t
	}

	fmt.Printf("sentNum=%d, receivedNum=%d, a=%d\n", nodeNum, receiveNum, a)
	if receiveNum != nodeNum {
		t.Errorf("%d Vs. %d", receiveNum, nodeNum)
	}
}
