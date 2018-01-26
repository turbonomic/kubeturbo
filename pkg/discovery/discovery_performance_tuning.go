package discovery

import (
	"fmt"
	"time"

	"github.com/turbonomic/kubeturbo/pkg/discovery/worker"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/processor"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
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
			name:     "New Framework",
			discFunc: dc.discoverWithNewFramework,
		},
		{
			name:     "New Framework Without Compliance",
			discFunc: dc.discoverWithNewFrameworkWithoutCompliance,
		},
	})
}

func (dc *K8sDiscoveryClient) discoverWithNewFrameworkWithoutCompliance() ([]*proto.EntityDTO, error) {
	nodes, err := dc.k8sClusterScraper.GetAllNodes()
	if err != nil {
		return nil, fmt.Errorf("Failed to get all nodes in the cluster: %s", err)
	}

	clusterProcessor := &processor.ClusterProcessor{
		ClusterInfoScraper: dc.k8sClusterScraper,
	}
	// Check connection to the cluster
	kubeCluster, err := dc.clusterProcessor.ConnectCluster(false)
	err = clusterProcessor.DiscoverCluster(kubeCluster)
	if err != nil {
		return nil, fmt.Errorf("Failed to process cluster: %s", err)
	}
	clusterSummary := repository.CreateClusterSummary(kubeCluster)

	workerCount := dc.dispatcher.Dispatch(nodes, clusterSummary)
	entityDTOs, _ := dc.resultCollector.Collect(workerCount)
	glog.V(3).Infof("Discovery workers have finished discovery work with %d entityDTOs built. Now performing service discovery...", len(entityDTOs))

	svcWorkerConfig := worker.NewK8sServiceDiscoveryWorkerConfig(dc.k8sClusterScraper)
	svcDiscWorker, err := worker.NewK8sServiceDiscoveryWorker(svcWorkerConfig)
	svcDiscResult := svcDiscWorker.Do(entityDTOs)
	if svcDiscResult.Err() != nil {
		glog.Errorf("Failed to discover services from current Kubernetes cluster with the new discovery framework: %s", svcDiscResult.Err())
	} else {
		entityDTOs = append(entityDTOs, svcDiscResult.Content()...)
	}

	return entityDTOs, nil
}
