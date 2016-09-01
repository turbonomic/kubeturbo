package externalprobebuilder

import (
	comm "github.com/vmturbo/vmturbo-go-sdk/pkg/communication"
	"github.com/vmturbo/vmturbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

type ExternalProbeBuilder struct{}

func (this *ExternalProbeBuilder) createAccountDefKubernetes() []*proto.AccountDefEntry {
	var acctDefProps []*proto.AccountDefEntry

	// target id
	targetIDAcctDefEntry := comm.NewAccountDefEntryBuilder("targetIdentifier", "Address",
		"IP of the kubernetes master", ".*", proto.AccountDefEntry_OPTIONAL, false).Create()
	acctDefProps = append(acctDefProps, targetIDAcctDefEntry)

	// username
	usernameAcctDefEntry := comm.NewAccountDefEntryBuilder("username", "Username",
		"Username of the kubernetes master", ".*", proto.AccountDefEntry_OPTIONAL, false).Create()
	acctDefProps = append(acctDefProps, usernameAcctDefEntry)

	// password
	passwdAcctDefEntry := comm.NewAccountDefEntryBuilder("password", "Password",
		"Password of the kubernetes master", ".*", proto.AccountDefEntry_OPTIONAL, true).Create()
	acctDefProps = append(acctDefProps, passwdAcctDefEntry)

	return acctDefProps
}

func (this *ExternalProbeBuilder) BuildProbes(probeType string) ([]*proto.ProbeInfo, error) {
	// 1. Construct the account definition for kubernetes.
	acctDefProps := this.createAccountDefKubernetes()

	// 2. Build supply chain.
	supplyChainFactory := &SupplyChainFactory{}
	templateDtos, err := supplyChainFactory.CreateSupplyChain()
	if err != nil {
		return nil, err
	}
	glog.V(2).Infof("Supply chain for Kubernetes is created.")

	// 3. construct the kubernetesProbe, the only probe supported.
	probeCat := "Container"
	k8sProbe := comm.NewProbeInfoBuilder(probeType, probeCat, templateDtos, acctDefProps).Create()

	// 4. Add kubernateProbe to probe, and that's the only probe supported in this client.
	var probes []*proto.ProbeInfo
	probes = append(probes, k8sProbe)

	return probes, err
}
