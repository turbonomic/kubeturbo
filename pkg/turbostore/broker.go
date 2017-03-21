package turbostore

import (
	"fmt"
	"sync"

	"github.com/golang/glog"
)

type Broker interface {
	Dispatch(key string, obj interface{}) error
	Subscribe(key string, consumer Consumer) error
	UnSubscribe(key string, consumer Consumer) error
	HasKey(key string) bool
}

// Not Scalable and Single Point Failure.
type PodBroker struct {
	// Broker maintains a map of topic and list of consumer who is listening on the topic.
	consumers map[string][]Consumer

	lock sync.RWMutex
}

func NewPodBroker() Broker {
	return &PodBroker{
		consumers: make(map[string][]Consumer),
	}
}

// Let consumer subscribe to the given topic.
func (b *PodBroker) Subscribe(key string, consumer Consumer) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	consumerList, exist := b.consumers[key]
	if !exist {
		consumerList = []Consumer{}
	}
	consumerList = append(consumerList, consumer)
	b.consumers[key] = consumerList
	glog.Infof("consumer %++v", b.consumers)

	return nil
}

// Remove consumer from listening the given topic.
func (b *PodBroker) UnSubscribe(key string, consumer Consumer) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	consumerList, exist := b.consumers[key]
	if !exist || len(consumerList) == 0 {
		return fmt.Errorf("No consumer is listening on %s", key)
	}
	foundIndex := -1
	for i, c := range b.consumers[key] {
		if consumer == c {
			foundIndex = i
		}
	}
	if foundIndex == -1 {
		fmt.Errorf("Consumer %v is not subscribed to listen on %s", consumer, key)
	}
	consumerList = append(consumerList[:foundIndex], consumerList[foundIndex+1:]...)
	if len(consumerList) == 0 {
		delete(b.consumers, key)
	}
	return nil
}

// Dispatch the received pod to consumers. If there are multiple consumers, always notify the first one.
func (b *PodBroker) Dispatch(key string, obj interface{}) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	glog.Infof("consumer %++v", b.consumers)

	consumerList, exist := b.consumers[key]
	if !exist || len(consumerList) == 0 {
		return fmt.Errorf("No consumer is listening on %s", key)
	}
	// Always only send to the first consumer.
	// TODO, more generic way?
	consumer := consumerList[0]
	consumer.Consume(obj)
	return nil
}

// Check if there is given topic in broker.
func (b *PodBroker) HasKey(key string) bool {
	b.lock.Lock()
	defer b.lock.Unlock()

	_, exist := b.consumers[key]
	return exist
}
