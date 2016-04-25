package reservation

import (
	"encoding/xml"
	"fmt"
	"strings"
)

/* A sample reservation response from VMT server:
<?xml version="1.0" encoding="ISO-8859-1"?>
<VirtualMachines>
	<ActionItem datastore="" datastoreState="" host="iperf-source1" hostState="Recommended" name="containerPodReservation123123_0_C0" status="OK" user="administrator" vdc="" vdcState=""/>
	<ActionItem datastore="" datastoreState="" host="iperf-source1" hostState="Recommended" name="containerPodReservation123123_1_C0" status="OK" user="administrator" vdc="" vdcState=""/>
</VirtualMachines>
*/
func decodeReservationResponse(content string) (*ServiceEntities, error) {
	// This is a temp solution. delete the encoding header.
	validStartIndex := strings.Index(content, ">")
	validContent := content[validStartIndex:]

	se := &ServiceEntities{}
	err := xml.Unmarshal([]byte(validContent), se)
	if err != nil {
		return nil, fmt.Errorf("Error decoding content: %s", err)
	}
	if se == nil {
		return nil, fmt.Errorf("Error decoding content. Result is null.")
	} else if len(se.ActionItems) < 1 {
		return nil, fmt.Errorf("Error decoding content. No ActionItem.")
	}
	return se, nil
}

func GetPodReservationDestination(content string) (string, error) {
	se, err := decodeReservationResponse(content)
	if err != nil {
		return "", err
	}
	if se.ActionItems[0].VM == "" {
		return "", fmt.Errorf("Reservation destination get from VMT server is null.")
	}

	// Now only support a single reservation each time.
	return se.ActionItems[0].VM, nil
}

func GetPodReservationStatus(content string) (string, error) {
	se, err := decodeReservationResponse(content)
	if err != nil {
		return "", err
	}
	if se.ActionItems[0].Status == "" {
		return "", fmt.Errorf("Status of reservation is null.")
	}
	return se.ActionItems[0].Status, nil
}
