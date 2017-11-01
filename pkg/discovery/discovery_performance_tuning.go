package discovery

import (
	"fmt"
	"time"

	"github.com/turbonomic/kubeturbo/pkg/discovery/old"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

type DiscoverTestElement struct {
	name     string
	discFunc discoverFunc
}

type DiscoverTestResult struct {
	name           string
	discoverTime   int64
	discoverResult []*proto.EntityDTO
	err            error
}

type discoverFunc func() ([]*proto.EntityDTO, error)

func compareTwoDiscoveryResults(old, new *DiscoverTestElement) {
	oldDiscoveryResult, err := old.discFunc()
	if err != nil {
		glog.Errorf("Failed to discovery with the old framework: %s", err)
	} else {
	}
	newDiscoveryResultDTOs, err := new.discFunc()
	if err != nil {
		glog.Errorf("Failed to use the new framework to discover current Kubernetes cluster: %s", err)
	}
	compareDiscoveryResults(oldDiscoveryResult, newDiscoveryResultDTOs)
}

func compareDifferentDiscoveryTime(testElems []*DiscoverTestElement) {
	var testResults []*DiscoverTestResult

	prevTime := time.Now()
	for _, elem := range testElems {
		_, err := elem.discFunc()
		if err != nil {
			glog.Errorf("Failed to discovery with %s: %s", elem.name, err)
		}
		discTime := time.Now().Sub(prevTime).Nanoseconds()
		testResults = append(testResults, &DiscoverTestResult{
			name:         elem.name,
			discoverTime: discTime,
		})
		prevTime = time.Now()
	}
	for _, result := range testResults {
		glog.Infof("%s discovery time: %dns", result.name, result.discoverTime)
	}
}

func (dc *K8sDiscoveryClient) discTimeCompare() {
	compareDifferentDiscoveryTime([]*DiscoverTestElement{
		{
			name:     "Old Framework",
			discFunc: dc.discoveryWithOldFramework,
		},
		{
			name:     "New Framework",
			discFunc: dc.discoverWithNewFramework,
		},
		{
			name:     "New Framework Without Compliance",
			discFunc: dc.discoverWithNewFrameworkWithoutCompliance,
		},
	})
}

func (dc *K8sDiscoveryClient) discoveryWithOldFramework() ([]*proto.EntityDTO, error) {
	//Discover the Kubernetes topology
	glog.V(2).Infof("Discovering Kubernetes cluster...")

	kubeProbe, err := old.NewK8sProbe(dc.config.k8sClusterScraper, dc.config.probeConfig)
	if err != nil {
		// TODO make error dto
		return nil, fmt.Errorf("Error creating Kubernetes discovery probe:%s", err.Error())
	}

	entityDtos, err := kubeProbe.Discovery()
	if err != nil {
		return nil, err
	}

	return entityDtos, nil
}

func (dc *K8sDiscoveryClient) discoverWithNewFrameworkWithoutCompliance() ([]*proto.EntityDTO, error) {
	nodes, err := dc.config.k8sClusterScraper.GetAllNodes()
	if err != nil {
		return nil, fmt.Errorf("Failed to get all nodes in the cluster: %s", err)
	}

	workerCount := dc.dispatcher.Dispatch(nodes)
	entityDTOs, _ := dc.resultCollector.Collect(workerCount)
	glog.V(3).Infof("Discovery workers have finished discovery work with %d entityDTOs built. Now performing service discovery...", len(entityDTOs))

	svcWorkerConfig := worker.NewK8sServiceDiscoveryWorkerConfig(dc.config.k8sClusterScraper)
	svcDiscWorker, err := worker.NewK8sServiceDiscoveryWorker(svcWorkerConfig)
	svcDiscResult := svcDiscWorker.Do(entityDTOs)
	if svcDiscResult.Err() != nil {
		glog.Errorf("Failed to discover services from current Kubernetes cluster with the new discovery framework: %s", svcDiscResult.Err())
	} else {
		entityDTOs = append(entityDTOs, svcDiscResult.Content()...)
	}

	return entityDTOs, nil
}
