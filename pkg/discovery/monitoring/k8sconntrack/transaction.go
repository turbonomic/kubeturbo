package k8sconntrack

type Transaction struct {
	ServiceId           string             `json:"serviceID,omitempty"`
	EndpointsCounterMap map[string]float64 `json:"endpointCounter,omitempty"`
}

func (t *Transaction) GetEndpointsCounterMap() map[string]float64 {
	return t.EndpointsCounterMap
}

type TransactionMetric struct {
	Capacity float64
	Used     float64
}
