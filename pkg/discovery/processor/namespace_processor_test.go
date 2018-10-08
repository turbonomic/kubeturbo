package processor

import (
	"github.com/stretchr/testify/assert"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

var (
	mockNamepaces = []struct {
		name string
	}{
		{"test-ns1"},
		{"test-ns2"},
		{"test-ns3"},
		{"test-ns4"},
	}

	mockQuotas = []struct {
		name      string
		namespace string
		cpu       float64
		mem       float64
	}{
		{"quota-1", "test-ns1", 4.0, 8010812.0},
		{"quota-2", "test-ns2", 4.0, 8010812.0},
		{"quota-31", "test-ns3", 4.0, 6010812.0},
		{"quota-32", "test-ns3", 2.0, 8010812.0},
	}
)

func createMockNamespaces() []*v1.Namespace {
	var nsList []*v1.Namespace
	for _, mockNamespace := range mockNamepaces {
		ns := &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: mockNamespace.name,
			},
		}
		nsList = append(nsList, ns)
	}
	return nsList
}
func createMockQuotas() map[string][]*v1.ResourceQuota {
	quotaMap := make(map[string][]*v1.ResourceQuota)
	for _, mockQuota := range mockQuotas {
		quota := &v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mockQuota.name,
				Namespace: mockQuota.namespace,
			},
		}
		quotaList, exists := quotaMap[mockQuota.namespace]
		if !exists {
			quotaList = []*v1.ResourceQuota{}
		}
		quotaList = append(quotaList, quota)
		quotaMap[mockQuota.namespace] = quotaList
	}
	return quotaMap
}
func TestProcessNamespaces(t *testing.T) {
	namespaceList := createMockNamespaces()
	quotaMap := createMockQuotas()
	ms := &MockClusterScrapper{
		mockGetNamespaces: func() ([]*v1.Namespace, error) {
			return namespaceList, nil
		},
		mockGetNamespaceQuotas: func() (map[string][]*v1.ResourceQuota, error) {
			return quotaMap, nil
		},
	}

	namespaceProcessor := &NamespaceProcessor{
		ClusterInfoScraper: ms,
		clusterName:        testClusterName,
	}

	nsMap, _ := namespaceProcessor.ProcessNamespaces()

	assert.Equal(t, len(nsMap), len(mockNamepaces))
	for _, ns := range nsMap {
		assert.NotNil(t, ns.Quota)
		assert.Equal(t, ns.Name, ns.Quota.Name)
	}
}
