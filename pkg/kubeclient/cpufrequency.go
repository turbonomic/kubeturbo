package kubeclient

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/golang/glog"
)

const (
	kubeturboNamespace = "KUBETURBO_NAMESPACE"
	DefaultCpuFreq     = float64(2000) //MHz
)

type NodeCpuFrequencyGetter struct {
	kubeClient   *kubernetes.Clientset
	node         *corev1.Node
	busyboxImage string
}

func NewNodeCpuFrequencyGetter(kubeClient *kubernetes.Clientset, busyboxImage string) *NodeCpuFrequencyGetter {
	return &NodeCpuFrequencyGetter{
		kubeClient:   kubeClient,
		busyboxImage: busyboxImage,
	}
}

func (n *NodeCpuFrequencyGetter) GetFrequency(nodeName string) (float64, error) {
	glog.V(4).Infof("Start query node frequency via pod for %s.", nodeName)

	// TODO: See if retries are needed
	namespace := os.Getenv(kubeturboNamespace)
	job, err := n.createJob(nodeName, namespace)
	if err != nil {
		return 0, err
	}

	jobName := job.Name
	defer n.waitForJobCleanup(jobName, namespace)

	err = n.waitForJob(jobName, namespace)
	if err != nil {
		return 0, fmt.Errorf("wait for job failed, node: %s : %v", nodeName, err)
	}

	pod, err := n.getJobsPod(jobName, namespace)
	if err != nil {
		return 0, fmt.Errorf("get cpufreq job pod failed for node: %s", nodeName)
	}

	return n.getCpufreqFromPodLog(pod)

}

func (n *NodeCpuFrequencyGetter) getCpufreqFromPodLog(pod *corev1.Pod) (float64, error) {
	req := n.kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{})
	logs, err := req.Stream()
	if err != nil {
		return 0, fmt.Errorf("error in opening stream")
	}
	defer logs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, logs)
	if err != nil {
		return 0, fmt.Errorf("error in copy pod logs")
	}
	line := buf.String()

	str := strings.Split(line, ":")
	if len(str) != 2 {
		return 0, fmt.Errorf("invalid cpufreq logs from pod")
	}

	cpufreq, err := strconv.ParseFloat(strings.TrimSpace(str[1]), 64)
	if err != nil {
		return 0, err
	}

	return cpufreq, nil

}

func (n *NodeCpuFrequencyGetter) waitForJob(jobName, namespace string) error {
	err := wait.Poll(1*time.Second, 20*time.Second, func() (bool, error) {
		j, err := n.kubeClient.BatchV1().Jobs(namespace).Get(jobName, metav1.GetOptions{})
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
		n.LogPodErrorEvents(jobName, pod)
	}

	return err
}

func (n *NodeCpuFrequencyGetter) LogPodErrorEvents(jobName string, pod *corev1.Pod) {
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
		deleteOptions := &metav1.DeleteOptions{PropagationPolicy: &deletePropagation}
		err := n.kubeClient.BatchV1().Jobs(namespace).Delete(jobName, deleteOptions)
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			glog.Warning(fmt.Printf("Error deleting cpufreq job: %s/%s: %v.", namespace, jobName, err))
		}

		// Retry
		return false, nil
	})
}

func (n *NodeCpuFrequencyGetter) getJobsPod(podNamePrefix, namespace string) (*corev1.Pod, error) {
	var pod *corev1.Pod
	pods, err := n.kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{})
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

func (n *NodeCpuFrequencyGetter) createJob(nodeName, namespace string) (*batchv1.Job, error) {
	job, err := n.kubeClient.BatchV1().Jobs(namespace).Create(getCpuFreqJobDefinition(nodeName, n.busyboxImage))
	if err != nil {
		return nil, fmt.Errorf("error creating cpufreq job for node: %s : %v", nodeName, err)
	}

	return job, err
}

func getCpuFreqJobDefinition(nodeName, busyboxImage string) *batchv1.Job {
	// There are no retries if the job fails it fails
	backoffLimit := int32(0)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kubeturbo-cpufreq-" + strconv.FormatInt(time.Now().UnixNano(), 32),
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeName:      nodeName,
					RestartPolicy: "Never",
					Containers: []corev1.Container{
						{
							Name:    "cpufreq",
							Image:   busyboxImage,
							Command: []string{`/bin/sh`},
							Args:    []string{"-c", `cat /proc/cpuinfo | grep -m 1 'cpu MHz'`},
						},
					},
				},
			},
			BackoffLimit: &backoffLimit,
		},
	}
}
