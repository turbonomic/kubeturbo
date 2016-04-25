package reservation

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"k8s.io/kubernetes/pkg/api"

	vmtapi "github.com/vmturbo/kubeturbo/pkg/api"
	"github.com/vmturbo/kubeturbo/pkg/helper"
	vmtmeta "github.com/vmturbo/kubeturbo/pkg/metadata"

	"github.com/golang/glog"
)

var (
	localTestingFlag bool = false

	actionTestingFlag bool = false

	localTestStitchingIP string = ""
)

func init() {
	flag, err := helper.LoadTestingFlag("./pkg/helper/testing_flag.json")
	if err != nil {
		glog.Errorf("Error initialize vmturbo package: %s", err)
		return
	}
	localTestingFlag = flag.LocalTestingFlag
}

type Reservation struct {
	Meta *vmtmeta.VMTMeta
}

func NewDeployment(meta *vmtmeta.VMTMeta) *Reservation {
	return &Reservation{
		Meta: meta,
	}
}

// use vmt api to get reservation destinations
// TODO for now only deal with one pod at a time
// But the result is a map. Will change later when deploy works.
func (this *Reservation) GetDestinationFromVmturbo(pod *api.Pod) (map[*api.Pod]string, error) {

	requestSpec := getRequestSpec(pod)

	// reservationResult is map[string]string -- [podName]nodeName
	// TODO !!!!!!! Now only support a single pod.
	reservationResult, err := this.RequestPlacement(pod.Name, requestSpec, nil)

	//-----------------The following is for the test purpose-----------------
	// After deploy framework works, it will get destination from vmt reservation api.
	// reservationResult := make(map[string]string)
	// dest, err := vmtScheduler.VMTScheduleHelper(pod)
	if err != nil {
		glog.Errorf("Cannot get deploy destination from vmturbo server")
		return nil, err
	}
	// reservationResult[pod.Name] = dest

	placementMap := make(map[*api.Pod]string)
	// currently only deal with one pod
	if nodeName, ok := reservationResult[pod.Name]; ok {
		if localTestingFlag {
			nodeName = "127.0.0.1"
		}
		placementMap[pod] = nodeName
	}
	return placementMap, nil
}

// Get the request specification, basically the pamameters that should be sent with post
func getRequestSpec(pod *api.Pod) map[string]string {
	requestSpec := make(map[string]string)
	requestSpec["reservation_name"] = "kubernetesReservationTest"
	requestSpec["num_instances"] = "1"
	// TODO, choose template name and template uuid based on pod resource limits.
	requestSpec["template_name"] = "DC5_1CxZMJkEEeCaJOYu5"
	requestSpec["templateUuids[]"] = "DC5_1CxZMJkEEeCaJOYu5"

	return requestSpec
}

// this method takes in a http get response for reservation and should return the reservation uuid, if there is any
func parseGetReservationResponse(podName, content string) (map[string]string, error) {
	if content == "" {
		return nil, fmt.Errorf("No valid reservation result.")
	}
	// Decode reservation content.
	dest, err := GetPodReservationDestination(content)
	if err != nil {
		return nil, err
	}
	glog.V(3).Infof("Deploy destination for Pod %s is %s", podName, dest)
	// TODO should parse the content. Currently don't know the correct get response content.
	pod2NodeMap := make(map[string]string)
	pod2NodeMap[podName] = dest
	return pod2NodeMap, nil
}

// Create the reservation specification and
// return map which has pod name as key and node name as value
func (this *Reservation) RequestPlacement(podName string, requestSpec, filterProperties map[string]string) (map[string]string, error) {
	extCongfix := make(map[string]string)
	extCongfix["Username"] = this.Meta.OpsManagerUsername
	extCongfix["Password"] = this.Meta.OpsManagerPassword
	vmturboApi := vmtapi.NewVmtApi(this.Meta.ServerAddress, extCongfix)

	glog.V(4).Info("Inside RequestPlacement")

	parameterString, err := buildReservationParameterString(requestSpec)
	if err != nil {
		return nil, err
	}

	reservationUUID, err := vmturboApi.Post("/reservations", parameterString)
	if err != nil {
		return nil, fmt.Errorf("Error posting reservations: %s", err)
	}
	reservationUUID = strings.Replace(reservationUUID, "\n", "", -1)
	glog.V(3).Infof("Reservation UUID is %s", string(reservationUUID))

	// TODO, do we want to wait for a predefined time or send send API requests multiple times.
	time.Sleep(2 * time.Second)
	getResponse, getRevErr := vmturboApi.Get("/reservations/" + reservationUUID)
	// After getting the destination, delete the reservation.
	deleteResponse, err := vmturboApi.Delete("/reservations/" + reservationUUID)
	if err != nil {
		// TODO, Should we return without placement?
		return nil, fmt.Errorf("Error deleting reservations destinations: %s", err)
	}
	glog.V(4).Infof("delete response of reservation %s is %s", reservationUUID, deleteResponse)
	if getRevErr != nil {
		return nil, fmt.Errorf("Error getting reservations destinations: %s", err)
	}
	pod2nodeMap, err := parseGetReservationResponse(podName, getResponse)
	if err != nil {
		return nil, fmt.Errorf("Error parsing reservation destination returned from VMTurbo server: %s", err)
	}

	return pod2nodeMap, nil
}

func buildReservationParameterString(requestSpec map[string]string) (string, error) {
	requestData := make(map[string]string)

	var requestDataBuffer bytes.Buffer

	if reservation_name, ok := requestSpec["reservation_name"]; !ok {
		glog.Errorf("reservation name is not registered")
		return "", fmt.Errorf("reservation_name has not been registered.")
	} else {
		requestData["reservationName"] = reservation_name
		requestDataBuffer.WriteString("?reservationName=")
		requestDataBuffer.WriteString(reservation_name)
		requestDataBuffer.WriteString("&")
	}

	if num_instances, ok := requestSpec["num_instances"]; !ok {
		glog.Errorf("num_instances not registered.")
		return "", fmt.Errorf("num_instances has not been registered.")
	} else {
		requestData["count"] = num_instances
		requestDataBuffer.WriteString("count=")
		requestDataBuffer.WriteString(num_instances)
		requestDataBuffer.WriteString("&")
	}

	if template_name, ok := requestSpec["template_name"]; !ok {
		glog.Errorf("template name is not registered")
		return "", fmt.Errorf("template_name has not been registered.")
	} else {
		requestData["templateName"] = template_name
		requestDataBuffer.WriteString("templateName=")
		requestDataBuffer.WriteString(template_name)
		requestDataBuffer.WriteString("&")
	}

	if templateUuids, ok := requestSpec["templateUuids[]"]; !ok {
		glog.Errorf("templateUuids is not specified.")
		return "", fmt.Errorf("templateUuids[] has not been registered.")
	} else {
		requestData["templateUuids[]"] = templateUuids
		requestDataBuffer.WriteString("templateUuids[]=")
		requestDataBuffer.WriteString(templateUuids)
	}

	s := requestDataBuffer.String()
	glog.V(4).Infof("parameters are %s", s)
	return s, nil
}
