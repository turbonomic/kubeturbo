package util

import (
	"context"

	api "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/applyconfigurations/core/v1"
	restclient "k8s.io/client-go/rest"
)

const (
	testPodName1 string = "pod1"
	testPodName2 string = "pod2"
	testPodUID1  string = "pod-UID1"
	testPodUID2  string = "pod-UID2"
	testPodUID3  string = "pod-UID3"
)

// MockPodInterface is used to mock podInterface for testing.
type MockPodInterface struct {
	Namespace string
}

func (p *MockPodInterface) Get(ctx context.Context, name string, opts metav1.GetOptions) (*api.Pod, error) {
	return nil, nil
}

func (p *MockPodInterface) List(ctx context.Context, opts metav1.ListOptions) (*api.PodList, error) {
	pod1 := api.Pod{}
	pod1.Name = testPodName1
	pod1.Namespace = p.Namespace
	pod1.UID = types.UID(testPodUID1)
	pod1.Status.Phase = api.PodRunning

	pod2 := api.Pod{}
	pod2.Name = testPodName1
	pod2.Namespace = p.Namespace
	pod2.UID = types.UID(testPodUID1)
	pod2.Status.Phase = api.PodRunning

	podList := api.PodList{
		Items: []api.Pod{
			pod1,
			pod2,
		},
	}

	return &podList, nil
}

func (p *MockPodInterface) Create(ctx context.Context, pod *api.Pod, opts metav1.CreateOptions) (*api.Pod, error) {
	return nil, nil
}

func (p *MockPodInterface) Update(ctx context.Context, pod *api.Pod, opts metav1.UpdateOptions) (*api.Pod, error) {
	return nil, nil
}

func (p *MockPodInterface) UpdateStatus(ctx context.Context, pod *api.Pod, opts metav1.UpdateOptions) (*api.Pod, error) {
	return nil, nil
}

func (p *MockPodInterface) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return nil
}

func (p *MockPodInterface) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return nil
}

func (p *MockPodInterface) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (p *MockPodInterface) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *api.Pod, err error) {
	return nil, nil
}

func (p *MockPodInterface) Bind(ctx context.Context, binding *api.Binding, co metav1.CreateOptions) error {
	return nil
}

func (p *MockPodInterface) Evict(ctx context.Context, eviction *policyv1beta1.Eviction) error {
	return nil
}

func (p *MockPodInterface) EvictV1(ctx context.Context, eviction *policyv1.Eviction) error {
	return nil
}

func (p *MockPodInterface) EvictV1beta1(ctx context.Context, eviction *policyv1beta1.Eviction) error {
	return nil
}

func (p *MockPodInterface) GetLogs(name string, opts *api.PodLogOptions) *restclient.Request {
	return nil
}

func (p *MockPodInterface) ProxyGet(scheme, name, port, path string, params map[string]string) restclient.ResponseWrapper {
	return nil
}
func (p *MockPodInterface) Apply(ctx context.Context, pod *corev1.PodApplyConfiguration, opts metav1.ApplyOptions) (result *api.Pod, err error) {
	return nil, nil
}
func (p *MockPodInterface) ApplyStatus(ctx context.Context, pod *corev1.PodApplyConfiguration, opts metav1.ApplyOptions) (result *api.Pod, err error) {
	return nil, nil
}

func (p *MockPodInterface) UpdateEphemeralContainers(ctx context.Context, podName string, pod *api.Pod, opts metav1.UpdateOptions) (*api.Pod, error) {
	return nil, nil
}
