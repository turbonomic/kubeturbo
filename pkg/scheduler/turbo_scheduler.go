package scheduler

import (
	"fmt"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/record"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/plugin/pkg/scheduler"
	"k8s.io/kubernetes/plugin/pkg/scheduler/metrics"

	"github.com/vmturbo/kubeturbo/pkg/scheduler/defaultscheduler"
	"github.com/vmturbo/kubeturbo/pkg/scheduler/vmtscheduler"

	"github.com/golang/glog"
)

type Config struct {
	Binder scheduler.Binder

	// Recorder is the EventRecorder to use
	Recorder record.EventRecorder
}

type TurboScheduler struct {
	config *Config

	vmtScheduler     *vmtscheduler.VMTScheduler
	defaultScheduler *defaultscheduler.DefaultScheduler
}

func NewTurboScheduler(kubeClient *client.Client, serverURL, username, password string) *TurboScheduler {
	config := &Config{
		Binder: &binder{kubeClient},
	}
	eventBroadcaster := record.NewBroadcaster()
	config.Recorder = eventBroadcaster.NewRecorder(api.EventSource{Component: "turboscheduler"})
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(kubeClient.Events(""))

	vmtSched := vmtscheduler.NewVMTScheduler(serverURL, username, password)
	glog.V(4).Infof("VMTScheduler is set: %++v", vmtSched)

	defaultSched := defaultscheduler.NewDefaultScheduler(kubeClient)
	glog.V(4).Infof("DefaultScheduler is set: %++v", defaultSched)

	return &TurboScheduler{
		config:           config,
		vmtScheduler:     vmtSched,
		defaultScheduler: defaultSched,
	}
}

// In Schedule, it always first try to get schedule destination from VMTScheduler.
// If fails then turn to use DefaultShceudler.
func (s *TurboScheduler) Schedule(pod *api.Pod) error {
	if s.vmtScheduler == nil {
		return fmt.Errorf("VMTScheduler has not been set. Must set before using TurboScheduler.")
	}
	glog.V(2).Infof("Use VMTScheduler to schedule Pod %s/%s", pod.Namespace, pod.Name)
	var placementMap map[*api.Pod]string
	placementMap, err := s.vmtScheduler.GetDestinationFromVmturbo(pod)
	if err != nil {
		glog.Warningf("Failed to schedule Pod %s/%s using VMTScheduler: %s", pod.Namespace, pod.Name, err)
		if s.defaultScheduler == nil {
			return fmt.Errorf("DefaultScheduler has not been set. Backup option is not available. "+
				"Failed to schedule Pod %s/%s", pod.Namespace, pod.Name)
		}
		glog.V(2).Infof("Use DefaultScheduler as an alternative option")
		dest, err := s.defaultScheduler.FindDestination(pod)
		if err != nil {
			return fmt.Errorf("Failed to schedule Pod %s/%s using DefaultScheduler: %s", pod.Namespace, pod.Name, err)
		}
		placementMap = make(map[*api.Pod]string)
		placementMap[pod] = dest
	}

	for podToBeScheduled, destinationNodeName := range placementMap {
		s.ScheduleTo(podToBeScheduled, destinationNodeName)
	}
	return nil
}

// Bind pod to destination node. dest is the name of the Node.
func (s *TurboScheduler) ScheduleTo(pod *api.Pod, dest string) {
	// if s.config.BindPodsRateLimiter != nil {
	// 	s.config.BindPodsRateLimiter.Accept()
	// }

	start := time.Now()
	defer func() {
		metrics.E2eSchedulingLatency.Observe(metrics.SinceInMicroseconds(start))
	}()
	metrics.SchedulingAlgorithmLatency.Observe(metrics.SinceInMicroseconds(start))

	b := &api.Binding{
		ObjectMeta: api.ObjectMeta{Namespace: pod.Namespace, Name: pod.Name},
		Target: api.ObjectReference{
			Kind: "Node",
			Name: dest,
		},
	}

	bindingStart := time.Now()
	err := s.config.Binder.Bind(b)

	if err != nil {
		glog.V(1).Infof("Failed to bind pod: %+v", err)
		s.config.Recorder.Eventf(pod, api.EventTypeNormal, "FailedScheduling", "Binding rejected: %v", err)
		return
	}
	metrics.BindingLatency.Observe(metrics.SinceInMicroseconds(bindingStart))
	s.config.Recorder.Eventf(pod, "Scheduled", "Successfully assigned %v to %v", pod.Name, dest)
}

type binder struct {
	*client.Client
}

// Bind just does a POST binding RPC.
func (b *binder) Bind(binding *api.Binding) error {
	glog.V(2).Infof("Attempting to bind %v to %v", binding.Name, binding.Target.Name)
	ctx := api.WithNamespace(api.NewContext(), binding.Namespace)
	return b.Post().Namespace(api.NamespaceValue(ctx)).Resource("bindings").Body(binding).Do().Error()
	// TODO: use Pods interface for binding once clusters are upgraded
	// return b.Pods(binding.Namespace).Bind(binding)
}
