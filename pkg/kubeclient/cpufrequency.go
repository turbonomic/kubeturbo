package kubeclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	set "github.com/deckarep/golang-set"
	"github.com/golang/glog"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
)

const (
	kubeturboNamespaceEnv = "KUBETURBO_NAMESPACE"
	defaultNamespace      = "default"
	defaultCpuFreq        = float64(2600) //MHz
	// cpufreq job by default is created every 10 mins
	// if there has been failures, a backoff delay would be added to retrials
	defaultInitialDelay = 10 * time.Minute
)

var (
	supportedOSArch = set.NewSet("linux.amd64", "linux.ppc64le")
)

// iNodeCpuFrequencyGetter defines an interface
type iNodeCpuFrequencyGetter interface {
	// GetFrequency is the shared method called by all concrete types
	GetFrequency(iNodeCpuFrequencyGetter, string) (float64, error)
	// GetJobCommand is a type specific method to get job command
	GetJobCommand() string
	// ParseCpuFrequency is a type specific method to parse CPU frequency from job log
	ParseCpuFrequency(string) (float64, error)
}

// NodeCpuFrequencyGetter defines an abstract type with default methods and fields shared by all concrete types
type NodeCpuFrequencyGetter struct {
	kubeClient      *kubernetes.Clientset
	busyboxImage    string
	imagePullSecret string
	backoffFailures map[string]*backoffFailure
}

type backoffFailure struct {
	// Number of times job failed since last success or since start
	failedTimes float64
	// Last failure timestamp
	lastTimestamp time.Time
	initialDelay  time.Duration
}

func newBackoff(delay time.Duration) *backoffFailure {
	return &backoffFailure{initialDelay: delay}
}

// Each failure would add a 2 exponent times initialDelay to backoff
// Any backoff call before that would return true meaning "back off"
func (b *backoffFailure) backoff() bool {
	return !b.lastTimestamp.IsZero() &&
		b.lastTimestamp.Add(time.Duration(math.Pow(2,
			b.failedTimes))*b.initialDelay).After(time.Now())
}

func (b *backoffFailure) setFailure() {
	if b.lastTimestamp.IsZero() {
		b.lastTimestamp = time.Now()
	}
	b.failedTimes++
}

func (b *backoffFailure) reset() {
	b.lastTimestamp = time.Time{}
	b.failedTimes = 0
}

// LinuxAmd64NodeCpuFrequencyGetter is a concrete type that embeds the NodeCpuFrequencyGetter and implements
// methods specific to linux amd64
type LinuxAmd64NodeCpuFrequencyGetter struct {
	NodeCpuFrequencyGetter
}

// GetJobCommand returns the command used to obtain CPU frequency on x64 linux
// TODO: Ideally we should use lscpu to get the averaged CPU speed across all CPUs on the node, instead of the CPU
//
//	speed of the first CPU
func (amd64 *LinuxAmd64NodeCpuFrequencyGetter) GetJobCommand() string {
	return `cat /proc/cpuinfo | grep -m 1 'cpu MHz'`
}

func (amd64 *LinuxAmd64NodeCpuFrequencyGetter) ParseCpuFrequency(jobLog string) (float64, error) {
	str := strings.Split(jobLog, ":")
	if len(str) != 2 {
		return 0, fmt.Errorf("invalid cpufreq logs from pod")
	}
	return strconv.ParseFloat(strings.TrimSpace(str[1]), 64)
}

// LinuxPpc64leNodeCpuFrequencyGetter is a concrete type that embeds the NodeCpuFrequencyGetter and implements
// methods specific to linux ppc64le
type LinuxPpc64leNodeCpuFrequencyGetter struct {
	NodeCpuFrequencyGetter
}

// GetJobCommand returns the command used to obtain CPU frequency on Power linux
// We have to use cat /proc/cpuinfo, as lscpu command does not return CPU speed on Power linux
func (ppc64le *LinuxPpc64leNodeCpuFrequencyGetter) GetJobCommand() string {
	return `cat /proc/cpuinfo | grep -m 1 'clock'`
}

// ParseCpuFrequency ParseCPUFrequency parses the CPU frequency for Power Linux
/*
# cat /proc/cpuinfo
processor       : 0
cpu             : POWER8E (raw), altivec supported
clock           : 3425.000000MHz
revision        : 2.1 (pvr 004b 0201)

processor       : 1
cpu             : POWER8E (raw), altivec supported
clock           : 3425.000000MHz
revision        : 2.1 (pvr 004b 0201)

timebase        : 512000000
platform        : pSeries
model           : IBM pSeries (emulated by qemu)
machine         : CHRP IBM pSeries (emulated by qemu)
*/
func (ppc64le *LinuxPpc64leNodeCpuFrequencyGetter) ParseCpuFrequency(jobLog string) (float64, error) {
	str := strings.Split(jobLog, ":")
	if len(str) != 2 {
		return 0, fmt.Errorf("invalid cpufreq logs from pod")
	}
	clock := strings.TrimSpace(str[1])
	// TODO: I cannot find the specification of /proc/cpuinfo output for Power linux. It is not clear if the MHz unit
	//   could potentially be different, such as GHz, on different node. For now, assume the unit is always MHz
	return strconv.ParseFloat(strings.TrimSuffix(clock, "MHz"), 64)
}

// NewNodeCpuFrequencyGetter creates an instance of the abstract type
func NewNodeCpuFrequencyGetter(kubeClient *kubernetes.Clientset, busyboxImage, imagePullSecret string) *NodeCpuFrequencyGetter {
	return &NodeCpuFrequencyGetter{
		kubeClient:      kubeClient,
		busyboxImage:    busyboxImage,
		imagePullSecret: imagePullSecret,
		backoffFailures: make(map[string]*backoffFailure),
	}
}

// GetFrequency obtains CPU frequency of a node by running a kubernetes job on that node, and then reading the
// /proc/cpuinfo file and parsing the CPU speed from the output. The job is cleaned up at the end.
func (n *NodeCpuFrequencyGetter) GetFrequency(i iNodeCpuFrequencyGetter, nodeName string) (float64, error) {
	glog.V(4).Infof("Start query node frequency via pod for %s.", nodeName)

	backoff, exists := n.backoffFailures[nodeName]
	if !exists {
		n.backoffFailures[nodeName] = newBackoff(defaultInitialDelay)
		backoff = n.backoffFailures[nodeName]
	}
	if backoff.backoff() {
		return 0, fmt.Errorf("backoff getting node cpu freq for: %s", nodeName)
	}

	// TODO: See if retries are needed
	namespace := os.Getenv(kubeturboNamespaceEnv)
	if namespace == "" {
		namespace = defaultNamespace
	}

	failed := false
	defer func() {
		if failed {
			backoff.setFailure()
		} else {
			backoff.reset()
		}
	}()
	job, err := n.createJob(i, namespace, nodeName)
	if err != nil {
		failed = true
		return 0, err
	}

	jobName := job.Name
	defer func() {
		err := n.waitForJobCleanup(jobName, namespace)
		if err != nil {
			glog.Warningf("Failed to clean up job %s/%s on node %s: %v", namespace, jobName, nodeName, err)
		}
	}()

	err = n.waitForJob(jobName, namespace)
	if err != nil {
		failed = true
		return 0, fmt.Errorf("wait for job %s/%s failed on node %s: %v", namespace, jobName, nodeName, err)
	}

	pod, err := n.getJobsPod(jobName, namespace)
	if err != nil {
		failed = true
		return 0, fmt.Errorf("get pod for job %s/%s failed on node %s: %v", namespace, jobName, nodeName, err)
	}

	cpufreq, err := n.getCpuFreqFromPodLog(i, pod)
	if err != nil {
		failed = true
		return 0, err
	}
	return cpufreq, nil
}

func (n *NodeCpuFrequencyGetter) getCpuFreqFromPodLog(i iNodeCpuFrequencyGetter, pod *corev1.Pod) (float64, error) {
	req := n.kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{})
	logs, err := req.Stream(context.TODO())
	if err != nil {
		return 0, fmt.Errorf("error in opening stream: %v", err)
	}
	defer func() {
		err := logs.Close()
		if err != nil {
			glog.Warningf("Failed to close log stream: %v", err)
		}
	}()
	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, logs)
	if err != nil {
		return 0, fmt.Errorf("error in copy pod logs: %v", err)
	}
	return i.ParseCpuFrequency(buf.String())
}

func (n *NodeCpuFrequencyGetter) waitForJob(jobName, namespace string) error {
	err := wait.Poll(1*time.Second, 30*time.Second, func() (bool, error) {
		j, err := n.kubeClient.BatchV1().Jobs(namespace).Get(context.TODO(), jobName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if j.Status.Succeeded == 1 {
			return true, nil
		}
		if j.Status.Failed == 1 {
			return false, fmt.Errorf("cpufreq job failed for job: %s/%s", namespace, jobName)
		}
		// Retry
		return false, nil
	})

	if err == wait.ErrWaitTimeout {
		// The job did not succeed/fail for 20 seconds, check pod errors if any.
		pod, err := n.getJobsPod(jobName, namespace)
		if err != nil {
			return err
		}
		n.logPodErrorEvents(pod)
	}

	return err
}

func (n *NodeCpuFrequencyGetter) logPodErrorEvents(pod *corev1.Pod) {
	// log a list of unique error events that belong to this pod
	podName := pod.Name
	podNamespace := pod.Namespace
	glog.Errorf("Error events on cpufreq pod: %s/%s", podNamespace, podName)
	podEvents := util.GetPodEvents(n.kubeClient, podNamespace, podName)
	for _, pe := range podEvents {
		if pe.EType == corev1.EventTypeWarning {
			glog.Errorf("%s/%s: %s", podNamespace, podName, pe.Message)
		}
	}
}

func (n *NodeCpuFrequencyGetter) waitForJobCleanup(jobName, namespace string) error {
	return wait.PollImmediate(1*time.Second, 20*time.Second, func() (bool, error) {
		deletePropagation := metav1.DeletePropagationBackground
		deleteOptions := metav1.DeleteOptions{PropagationPolicy: &deletePropagation}
		err := n.kubeClient.BatchV1().Jobs(namespace).Delete(context.TODO(), jobName, deleteOptions)
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			glog.Warningf("Error deleting cpufreq job: %s/%s: %v.", namespace, jobName, err)
		}
		// Retry
		return false, nil
	})
}

func (n *NodeCpuFrequencyGetter) getJobsPod(podNamePrefix, namespace string) (*corev1.Pod, error) {
	var pod *corev1.Pod
	pods, err := n.kubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, p := range pods.Items {
		// A better option would be to check parent per pod, but this is faster
		if strings.HasPrefix(p.Name, podNamePrefix) {
			pod = &p
			break
		}
	}
	if pod == nil {
		return nil, fmt.Errorf("find pod for job: %s/%s failed", namespace, podNamePrefix)
	}

	return pod, nil
}

func (n *NodeCpuFrequencyGetter) createJob(i iNodeCpuFrequencyGetter, namespace, nodeName string) (*batchv1.Job, error) {
	job, err := n.kubeClient.BatchV1().Jobs(namespace).
		Create(context.TODO(), n.getCpuFreqJobDefinition(i, nodeName), metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("error creating cpufreq job for node: %s: %v", nodeName, err)
	}
	return job, nil
}

func (n *NodeCpuFrequencyGetter) getCpuFreqJobDefinition(i iNodeCpuFrequencyGetter, nodeName string) *batchv1.Job {
	// There are no retries if the job fails it fails
	backoffLimit := int32(0)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kubeturbo-cpufreq-" + strconv.FormatInt(time.Now().UnixNano(), 32),
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						// This is to ensure istio sidecars are not injected into this jobs pod
						// Ref k8s [No solution]: https://github.com/kubernetes/kubernetes/issues/25908
						// Ref Istio [Workaround] : https://github.com/istio/istio/issues/11045
						"sidecar.istio.io/inject": "false",
					},
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: []corev1.LocalObjectReference{
						{
							Name: n.imagePullSecret,
						},
					},
					NodeName:      nodeName,
					RestartPolicy: "Never",
					Containers: []corev1.Container{
						{
							Name:            "cpufreq",
							Image:           n.busyboxImage,
							Command:         []string{`/bin/sh`},
							Args:            []string{"-c", i.GetJobCommand()},
							ImagePullPolicy: "IfNotPresent",
						},
					},
				},
			},
			BackoffLimit: &backoffLimit,
		},
	}
}
