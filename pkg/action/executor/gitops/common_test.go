package gitops

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func TestCompareYamlContent(t *testing.T) {
	testCases := []struct {
		test       string
		configYaml string
		liveYaml   string
		modified   bool
	}{
		{
			test: "live resource matches config yaml",
			configYaml: `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations: null
  labels:
    app: bee
  name: beekman-one-more
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bee
  template:
    metadata:
      labels:
        app: bee
    spec:
      containers:
      - env:
        - name: RUN_TYPE
          value: cpu
        - name: CPU_PERCENT
          value: "95"
        image: beekman9527/cpumemload:latest
        imagePullPolicy: Always
        name: beekman-1
        resources:
          limits:
            cpu: 11m
            memory: 28Mi
          requests:
            cpu: 10m
            memory: 10Mi
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      imagePullSecrets:
      - name: docker-credentials
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      terminationGracePeriodSeconds: 30
`,
			liveYaml: `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations: null
  labels:
    app: bee
  name: beekman-one-more
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bee
  template:
    metadata:
      labels:
        app: bee
    spec:
      containers:
      - env:
        - name: RUN_TYPE
          value: cpu
        - name: CPU_PERCENT
          value: "95"
        image: beekman9527/cpumemload:latest
        imagePullPolicy: Always
        name: beekman-1
        resources:
          limits:
            cpu: 11m
            memory: 28Mi
          requests:
            cpu: 10m
            memory: 10Mi
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      imagePullSecrets:
      - name: docker-credentials
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      terminationGracePeriodSeconds: 30
`,
			modified: false,
		},
		{
			test: "live resource with defaulted additional data matches config yaml",
			configYaml: `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations: null
  labels:
    app: bee
  name: beekman-one-more
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bee
  template:
    metadata:
      labels:
        app: bee
    spec:
      imagePullSecrets:
      - name: docker-credentials
      containers:
      - name: beekman-1
        image: beekman9527/cpumemload:latest
        env:
        - name: RUN_TYPE
          value: "cpu"
        - name: CPU_PERCENT
          value: "95"
        resources:
          requests:
            memory: "10Mi"
            cpu: "10m"
          limits:
            memory: "28Mi"
            cpu: "11m"
`,
			liveYaml: `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    deployment.kubernetes.io/revision: '12'
    kubectl.kubernetes.io/last-applied-configuration: >
      {"apiVersion":"apps/v1","kind":"Deployment","metadata":{"annotations":{},"labels":{"app":"bee","app.kubernetes.io/instance":"gitops-pr-test"},"name":"beekman-one-more","namespace":"argo-test"},"spec":{"replicas":1,"selector":{"matchLabels":{"app":"bee"}},"template":{"metadata":{"labels":{"app":"bee"}},"spec":{"containers":[{"env":[{"name":"RUN_TYPE","value":"cpu"},{"name":"CPU_PERCENT","value":"95"}],"image":"beekman9527/cpumemload:latest","imagePullPolicy":"Always","name":"beekman-1","resources":{"limits":{"cpu":"11m","memory":"28Mi"},"requests":{"cpu":"10m","memory":"10Mi"}},"terminationMessagePath":"/dev/termination-log","terminationMessagePolicy":"File"}],"dnsPolicy":"ClusterFirst","imagePullSecrets":[{"name":"docker-credentials"}],"restartPolicy":"Always","schedulerName":"default-scheduler","securityContext":{},"terminationGracePeriodSeconds":30}}}}
  creationTimestamp: '2022-05-10T06:56:49Z'
  generation: 12
  labels:
    app: bee
  name: beekman-one-more
  namespace: argo-test
  resourceVersion: '154352815'
  selfLink: /apis/apps/v1/namespaces/argo-test/deployments/beekman-one-more
  uid: 1c05b221-4786-4047-9ef6-4b0ca4d92566
spec:
  progressDeadlineSeconds: 600
  replicas: 1
  revisionHistoryLimit: 10
  selector:
    matchLabels:
      app: bee
  strategy:
    rollingUpdate:
      maxSurge: 25%
      maxUnavailable: 25%
    type: RollingUpdate
  template:
    metadata:
      creationTimestamp: null
      labels:
        app: bee
    spec:
      containers:
        - env:
            - name: RUN_TYPE
              value: cpu
            - name: CPU_PERCENT
              value: '95'
          image: 'beekman9527/cpumemload:latest'
          imagePullPolicy: Always
          name: beekman-1
          resources:
            limits:
              cpu: 11m
              memory: 28Mi
            requests:
              cpu: 10m
              memory: 10Mi
          terminationMessagePath: /dev/termination-log
          terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      imagePullSecrets:
        - name: docker-credentials
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      terminationGracePeriodSeconds: 30
status:
  availableReplicas: 1
  conditions:
    - lastTransitionTime: '2022-06-06T04:10:36Z'
      lastUpdateTime: '2022-06-06T04:10:36Z'
      message: Deployment has minimum availability.
      reason: MinimumReplicasAvailable
      status: 'True'
      type: Available
    - lastTransitionTime: '2022-07-26T13:09:08Z'
      lastUpdateTime: '2022-08-02T13:14:28Z'
      message: ReplicaSet "beekman-one-more-784c88b9b" has successfully progressed.
      reason: NewReplicaSetAvailable
      status: 'True'
      type: Progressing
  observedGeneration: 12
  readyReplicas: 1
  replicas: 1
  updatedReplicas: 1
`,
			modified: false,
		},
		{
			test: "live resource matches config yaml with different unit resolution but same resulting values",
			configYaml: `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations: null
  labels:
    app: bee
  name: beekman-one-more
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bee
  template:
    metadata:
      labels:
        app: bee
    spec:
      containers:
      - env:
        - name: RUN_TYPE
          value: cpu
        - name: CPU_PERCENT
          value: "95"
        image: beekman9527/cpumemload:latest
        imagePullPolicy: Always
        name: beekman-1
        resources:
          limits:
            cpu: 1000m
            memory: 28Mi
          requests:
            cpu: 10m
            memory: 10Mi
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      imagePullSecrets:
      - name: docker-credentials
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      terminationGracePeriodSeconds: 30
`,
			liveYaml: `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations: null
  labels:
    app: bee
  name: beekman-one-more
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bee
  template:
    metadata:
      labels:
        app: bee
    spec:
      containers:
      - env:
        - name: RUN_TYPE
          value: cpu
        - name: CPU_PERCENT
          value: "95"
        image: beekman9527/cpumemload:latest
        imagePullPolicy: Always
        name: beekman-1
        resources:
          limits:
            cpu: "1"
            memory: 28Mi
          requests:
            cpu: 10m
            memory: 10Mi
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      imagePullSecrets:
      - name: docker-credentials
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      terminationGracePeriodSeconds: 30
`,
			modified: false,
		},
		{
			test: "live resource does not match config yaml when some value is a mismatch (resource mismatch)",
			configYaml: `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations: null
  labels:
    app: bee
  name: beekman-one-more
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bee
  template:
    metadata:
      labels:
        app: bee
    spec:
      containers:
      - env:
        - name: RUN_TYPE
          value: cpu
        - name: CPU_PERCENT
          value: "95"
        image: beekman9527/cpumemload:latest
        imagePullPolicy: Always
        name: beekman-1
        resources:
          limits:
            cpu: 1001m
            memory: 28Mi
          requests:
            cpu: 10m
            memory: 10Mi
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      imagePullSecrets:
      - name: docker-credentials
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      terminationGracePeriodSeconds: 30
`,
			liveYaml: `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations: null
  labels:
    app: bee
  name: beekman-one-more
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bee
  template:
    metadata:
      labels:
        app: bee
    spec:
      containers:
      - env:
        - name: RUN_TYPE
          value: cpu
        - name: CPU_PERCENT
          value: "95"
        image: beekman9527/cpumemload:latest
        imagePullPolicy: Always
        name: beekman-1
        resources:
          limits:
            cpu: "1"
            memory: 28Mi
          requests:
            cpu: 10m
            memory: 10Mi
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      imagePullSecrets:
      - name: docker-credentials
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      terminationGracePeriodSeconds: 30
`,
			modified: true,
		},
		{
			test: "live resource does not match config yaml when some value is a mismatch (replicas mismatch)",
			configYaml: `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations: null
  labels:
    app: bee
  name: beekman-one-more
spec:
  replicas: 2
  selector:
    matchLabels:
      app: bee
  template:
    metadata:
      labels:
        app: bee
    spec:
      containers:
      - env:
        - name: RUN_TYPE
          value: cpu
        - name: CPU_PERCENT
          value: "95"
        image: beekman9527/cpumemload:latest
        imagePullPolicy: Always
        name: beekman-1
        resources:
          limits:
            cpu: "1"
            memory: 28Mi
          requests:
            cpu: 10m
            memory: 10Mi
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      imagePullSecrets:
      - name: docker-credentials
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      terminationGracePeriodSeconds: 30
`,
			liveYaml: `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations: null
  labels:
    app: bee
  name: beekman-one-more
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bee
  template:
    metadata:
      labels:
        app: bee
    spec:
      containers:
      - env:
        - name: RUN_TYPE
          value: cpu
        - name: CPU_PERCENT
          value: "95"
        image: beekman9527/cpumemload:latest
        imagePullPolicy: Always
        name: beekman-1
        resources:
          limits:
            cpu: "1"
            memory: 28Mi
          requests:
            cpu: 10m
            memory: 10Mi
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      imagePullSecrets:
      - name: docker-credentials
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      terminationGracePeriodSeconds: 30
`,
			modified: true,
		},
	}

	for _, testCase := range testCases {
		liveRes, err := DecodeYaml(testCase.liveYaml)
		if err != nil {
			t.Errorf("Test: %v \nError decoding live yaml: %v", testCase.test, err)
		}

		liveReplicas, found, err := unstructured.NestedFieldCopy(liveRes.Object, "spec", "replicas")
		if err != nil || !found {
			t.Errorf("Test: %v \nError retrieving podSpec from the live res: %v", testCase.test, err)
		}

		livePodSpec, found, err := unstructured.NestedFieldCopy(liveRes.Object, "spec", "template", "spec")
		if err != nil || !found {
			t.Errorf("Test: %v \nError retrieving podSpec from the live res: %v", testCase.test, err)
		}

		modified, patchedResult, err := PatchYamlContent(testCase.configYaml, liveReplicas.(int64), livePodSpec.(map[string]interface{}))
		if err != nil {
			t.Errorf("Test: %v \nError patching yaml content %v", testCase.test, err)
		}

		if modified != testCase.modified {
			data, _ := yaml.JSONToYAML(patchedResult)

			t.Errorf("Diff failed test: %s. expected modified: %v, got modified: %v. \nconfig yaml: \n%v\n expected patched yaml: \n%v \n%v",
				testCase.test, testCase.modified, modified, testCase.configYaml, string(data), string(patchedResult))
		}
	}
}
