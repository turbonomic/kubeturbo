package metadata

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/golang/glog"
)

const (
	TARGET_TYPE       string = "Kubernetes"
	USERNAME          string = "kubernetes_user"
	TARGET_IDENTIFIER string = "my_k8s"
	PASSWORD          string = "fake_password"

	//WebSocket related
	LOCAL_ADDRESS    string = "http://172.16.201.167/"
	WS_SERVER_USRN   string = "vmtRemoteMediation"
	WS_SERVER_PASSWD string = "vmtRemoteMediation"
)

type VMTMeta struct {
	ServerAddress      string
	TargetType         string
	NameOrAddress      string
	Username           string
	TargetIdentifier   string
	Password           string
	LocalAddress       string
	WebSocketUsername  string
	WebSocketPassword  string
	OpsManagerUsername string
	OpsManagerPassword string
}

// Create a new VMTMeta from file. ServerAddress, NameOrAddress of Kubernetes target, Ops Manager Username and
// Ops Manager Password should be set by user. Other fields have default values and can be overrided.
func NewVMTMeta(metaConfigFilePath string) (*VMTMeta, error) {
	meta := &VMTMeta{
		TargetType:        TARGET_TYPE,
		Username:          USERNAME,
		TargetIdentifier:  TARGET_IDENTIFIER,
		Password:          PASSWORD,
		LocalAddress:      LOCAL_ADDRESS,
		WebSocketUsername: WS_SERVER_USRN,
		WebSocketPassword: WS_SERVER_PASSWD,
	}

	glog.V(4).Infof("Now read configration from %s", metaConfigFilePath)
	metaConfig := readConfig(metaConfigFilePath)

	if metaConfig.ServerAddress != "" {
		meta.ServerAddress = metaConfig.ServerAddress
		glog.V(2).Infof("VMTurbo Server Address is %s", meta.ServerAddress)

	} else {
		return nil, fmt.Errorf("Error getting VMTurbo server address.")
	}

	if metaConfig.TargetIdentifier != "" {
		meta.TargetIdentifier = metaConfig.TargetIdentifier
	}
	glog.V(3).Infof("TargetIdentifier is %s", meta.TargetIdentifier)

	if metaConfig.NameOrAddress != "" {
		meta.NameOrAddress = metaConfig.NameOrAddress
		glog.V(3).Infof("NameOrAddress is %s", meta.NameOrAddress)
	} else {
		return nil, fmt.Errorf("Error getting NameorAddress for Kubernetes Probe.")
	}

	if metaConfig.Username != "" {
		meta.Username = metaConfig.Username
	}

	if metaConfig.TargetType != "" {
		meta.TargetType = metaConfig.TargetType
	}

	if metaConfig.Password != "" {
		meta.Password = metaConfig.Password
	}

	if metaConfig.LocalAddress != "" {
		meta.LocalAddress = metaConfig.LocalAddress
	}

	if metaConfig.OpsManagerUsername != "" {
		meta.OpsManagerUsername = metaConfig.OpsManagerUsername
	} else {
		return nil, fmt.Errorf("Error getting VMTurbo Ops Manager Username.")
	}

	if metaConfig.OpsManagerPassword != "" {
		meta.OpsManagerPassword = metaConfig.OpsManagerPassword
	} else {
		return nil, fmt.Errorf("Error getting VMTurbo Ops Manager Password.")
	}

	return meta, nil
}

// Get the config from file.
func readConfig(path string) VMTMeta {
	file, e := ioutil.ReadFile(path)
	if e != nil {
		glog.Errorf("File error: %v\n", e)
		os.Exit(1)
	}
	var metaData VMTMeta
	json.Unmarshal(file, &metaData)
	glog.V(4).Infof("Results: %v\n", metaData)
	return metaData
}
