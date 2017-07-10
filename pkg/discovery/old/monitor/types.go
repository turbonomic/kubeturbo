package monitor

type Transaction struct {
	ServiceId           string             `json:"serviceID,omitempty"`
	EndpointsCounterMap map[string]float64 `json:"endpointCounter,omitempty"`
}

func (this *Transaction) GetEndpointsCounterMap() map[string]float64 {
	return this.EndpointsCounterMap
}
