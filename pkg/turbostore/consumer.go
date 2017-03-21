package turbostore

import (
	"errors"

	"k8s.io/kubernetes/pkg/api"

	"github.com/golang/glog"
)

type Consumer interface {
	// Get the UUID of Consumer.
	GetUID() (string, error)

	// Get the obj from broker and process the obj.
	Consume(obj interface{}) error

	// Stop listening any event on given key.
	Leave(key string, broker Broker) error
}

// Not Scalable and Single Point Failure.
type PodConsumer struct {
	// UID of a PodConsumer cannot be nil or empty string.
	uid string

	receivedPodChan chan *api.Pod
}

func NewPodConsumer(uid, key string, broker Broker) *PodConsumer {
	c := &PodConsumer{
		uid: uid,

		receivedPodChan: make(chan *api.Pod),
	}
	broker.Subscribe(key, c)
	glog.V(3).Infof("Pod consumer listening on %s has subscribed to broker.", key)
	return c
}

func (c *PodConsumer) GetUID() (string, error) {
	if c.uid == "" {
		return "", errors.New("Invalid UID")
	}
	return c.uid, nil
}

// Receive a Pod and forward to receiving channel.
func (c *PodConsumer) Consume(obj interface{}) error {
	pod, ok := obj.(*api.Pod)
	if !ok {
		glog.Errorf("Failed to consume: %v is not a Pod", obj)
	}
	c.receivedPodChan <- pod
	return nil
}

func (c *PodConsumer) Leave(key string, broker Broker) error {
	err := broker.UnSubscribe(key, c)
	if err != nil {
		return err
	}
	close(c.receivedPodChan)
	glog.V(3).Infof("Pod consumer listening on %s has unsubscribed.", key)
	return nil
}

// Expose the receiving channel.
func (c *PodConsumer) WaitPod() <-chan *api.Pod {
	return c.receivedPodChan
}
