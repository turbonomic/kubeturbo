package api

type ResourceType string

const (
	Resource_Type_Reservation     ResourceType = "reservations"
	Resource_Type_Targets         ResourceType = "targets"
	Resource_Type_Target          ResourceType = "target"
	Resource_Type_Probe           ResourceType = "probe"
	Resource_Type_External_Target ResourceType = "externaltargets"
)
