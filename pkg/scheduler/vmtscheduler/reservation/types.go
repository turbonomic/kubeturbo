package reservation

import (
	"encoding/xml"
)

type ServiceEntities struct {
	XMLName     xml.Name     `xml:"ServiceEntities"`
	ActionItems []ActionItem `xml:"ActionItem"`
}

type ActionItem struct {
	Datastore      string `xml:"datastore,attr"`
	DataStoreState string `xml:"datastoreState,attr"`
	Host           string `xml:"host,attr"`
	HostState      string `xml:"hostState,attr"`
	Name           string `xml:"name,attr"`
	Status         string `xml:"status,attr"`
	User           string `xml:"user,attr"`
	Vdc            string `xml:"vdc,attr"`
	VdcState       string `xml:"vdcState,attr"`
	VM             string `xml:"vm,attr"`
	VMState        string `xml:"vmState,attr"`
}
