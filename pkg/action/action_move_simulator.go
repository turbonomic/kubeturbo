package action

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
)

// This struct is for move action simulator. The simulation process is as follows:
// 1. action simulator specifies which pod to move, where to move, and the message id of the move request from server
//    writing all the necessary info to /tmp/dat1.
//    the format is : action\tpod\tdestination\tmsgId, eg. "move	pod_name	10.10.101.1		1"
// 2. simulator calls MovePod() action, deleting the specified pod
// 3. the replication controller will create a new pod with the same label, meeting the desire number of replica
// 4. now in vmt service, getNextPod() will catch the new created pod. Call this func.
// 5. here it checks whether there is any entry in /tmp/dat1. If there is, retrieve destination from the file
// 6. before return the destination, it should clear the entry in /tmp/dat1.
// 7. if there's no entry, return an empty string.

type MoveSimulator struct{}

const (
	filePath string = "/tmp/dat1"
)

// Simulate the move action. Forming the move action description.
func (ms *MoveSimulator) SimulateMove(action, podToMove, destinationNode string, msgID int32) {
	var buffer bytes.Buffer
	buffer.WriteString(action)
	buffer.WriteString("\t")
	buffer.WriteString(podToMove)
	buffer.WriteString("\t")
	buffer.WriteString(destinationNode)

	msgIDString := strconv.Itoa(int(msgID))
	buffer.WriteString("\t")
	buffer.WriteString(msgIDString)

	actionDescription := buffer.String()
	now := time.Now()
	formattedNow := now.Format(time.RFC1123)
	actionDescription = actionDescription + "\t" + formattedNow + "\n"
	fmt.Printf("the action is %s.\n", actionDescription)
	// write(actionDescription, "/tmp/dat1")
	write(actionDescription, filePath)

}

// Check whether a newly created pod is as a result of move action.
// If it is, then return the move desintaion and the msgID of the move request;
// Otherwise, return an empty stirng and -1.
func (ms *MoveSimulator) IsMovePod() (destination string, msgID int32) {
	// filePath := "/tmp/dat1"
	entry, err := read(filePath)
	if err != nil {
		glog.Errorf("Error reading in %s: %s", filePath, err)
		return "", -1
	}
	content := strings.Split(string(entry), "\t")
	if len(content) < 4 {
		return "", -1
	}
	destination = content[2]
	number, err := strconv.ParseInt(content[3], 10, 32)
	if err != nil {
		glog.Errorf("Error getting message id: %s", err)
		return "", -1
	}
	msgID = int32(number)
	err = write("", filePath)
	if err != nil {
		glog.Errorf("Error flushing %s: %s", filePath, err)
		return "", -1
	}
	return

}

// Write action description to the specified file.
func write(actionDescription, filePath string) error {
	// To start, here’s how to dump a string (or just bytes) into a file.
	ad := []byte(actionDescription)
	err := ioutil.WriteFile(filePath, ad, 0644)
	if err != nil {
		return err
	}
	return nil
}

// Read action description from the specified file.
func read(filePath string) (string, error) {
	dat, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	fmt.Print(string(dat))
	return string(dat), nil
}
