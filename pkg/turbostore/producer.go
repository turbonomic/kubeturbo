package turbostore

import (
	"fmt"
)

type Producer interface {
	Produce(broker Broker, key string, obj interface{}) error
}

type KeyFunc func(interface{}) (string, error)

type PodProducer struct{}

func (p *PodProducer) Produce(broker Broker, key string, obj interface{}) error {
	if err := broker.Dispatch(key, obj); err != nil {
		return fmt.Errorf("Failed to produce: %s", err)
	}
	return nil
}
