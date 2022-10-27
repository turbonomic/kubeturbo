package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/v42/github"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// createCommit represents the body of a CreateCommit request.
type createCommit struct {
	Author    *github.CommitAuthor `json:"author,omitempty"`
	Committer *github.CommitAuthor `json:"committer,omitempty"`
	Message   *string              `json:"message,omitempty"`
	Tree      *string              `json:"tree,omitempty"`
	Parents   []string             `json:"parents,omitempty"`
	Signature *string              `json:"signature,omitempty"`
}

// updateRefRequest represents the payload for updating a reference.
type updateRefRequest struct {
	SHA   *string `json:"sha"`
	Force *bool   `json:"force"`
}

func TestGitHandler_PushCommit(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	author := "a"
	email := "e"
	path := "p"
	msg := "Turbonomic Action: update yaml file " + path

	// GetCommit() handling
	mux.HandleFunc("/repos/o/r/commits/existing-commit-sha-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Error in METHOD. Expected: GET, Got: %s", r.Method)
		}

		fmt.Fprint(w, `
		{
		  "sha":"existing-commit-sha-1",
		  "commit":{"sha":"existing-commit-sha-1", "message":"Commit Message."},
		  "author":{"name":"n"}
		}`)
	})

	// CreateCommit() handling
	mux.HandleFunc("/repos/o/r/git/commits", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Error in METHOD. Expected: POST, Got: %s", r.Method)
		}

		commited := new(createCommit)
		json.NewDecoder(r.Body).Decode(commited)

		if commited.Author.GetName() != author {
			fmt.Fprintf(w, "Wrong Author Name. Expected: %s, Got: %s", email, commited.Author.GetName())
		}
		if commited.Author.GetEmail() != email {
			fmt.Fprintf(w, "Wrong Email. Expected: %s, Got: %s", email, commited.Author.GetEmail())
		}
		if *commited.Message != msg {
			fmt.Fprintf(w, "Wrong commit msg. Expected: %s, Got: %s", msg, *commited.Message)
		}

		fmt.Fprint(w, `
		{
		  "sha":"new-commit-sha-1",
		  "commit":{"sha":"new-commit-sha-1", "message":"New Commit Message."},
		  "author":{"name":"n"}
		}`)
	})

	// UpdateRef() handling
	mux.HandleFunc("/repos/o/r/git/refs/heads/b", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("Error in METHOD. Expected: PATCH, Got: %s", r.Method)
		}

		updateRef := new(updateRefRequest)
		json.NewDecoder(r.Body).Decode(updateRef)
		if *updateRef.SHA != "new-commit-sha-1" {
			fmt.Fprintf(w, "Wrong update ref commit sha. Expected: new-commit-sha-1, Got: %s", *updateRef.SHA)
		}

		fmt.Fprint(w, `
		  {
		    "ref": "refs/heads/b",
		    "url": "https://api.github.com/repos/o/r/git/refs/heads/b",
		    "object": {
		      "type": "commit",
		      "sha": "new-commit-sha-1",
		      "url": "https://api.github.com/repos/o/r/git/commits/new-commit-sha-1"
		    }
		  }`)
	})

	ctx := context.Background()
	handler := &GitHandler{
		ctx:         ctx,
		client:      client,
		user:        "o",
		repo:        "r",
		baseBranch:  "b",
		path:        path,
		commitUser:  author,
		commitEmail: email,
	}

	ref := &github.Reference{
		Ref: String("refs/heads/b"),
		URL: String("https://api.github.com/repos/o/r/git/refs/heads/b"),
		Object: &github.GitObject{
			Type: String("commit"),
			SHA:  String("existing-commit-sha-1"),
			URL:  String("https://api.github.com/repos/o/r/git/commits/existing-commit-sha-1"),
		},
	}

	tree := &github.Tree{
		SHA: String("sha-2"),
		Entries: []*github.TreeEntry{
			{
				Path: String("file.rb"),
				Mode: String("100644"),
				Type: String("blob"),
				Size: Int(132),
				SHA:  String("sha-3"),
			},
		},
		Truncated: nil,
	}

	newRef, err := handler.pushCommit(ref, tree)
	if err != nil {
		t.Errorf("Git Handler pushCommit returned error: %v", err)
	}

	wantRef := &github.Reference{
		Ref: String("refs/heads/b"),
		URL: String("https://api.github.com/repos/o/r/git/refs/heads/b"),
		Object: &github.GitObject{
			Type: String("commit"),
			SHA:  String("new-commit-sha-1"),
			URL:  String("https://api.github.com/repos/o/r/git/commits/new-commit-sha-1"),
		},
	}

	if !cmp.Equal(newRef, wantRef) {
		t.Errorf("Git.CreateTree returned %+v, want %+v", newRef, wantRef)
	}

}

func TestGitHandler_CreateNewBranch(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	author := "a"
	email := "e"
	path := "p"

	// GetRef() handling
	mux.HandleFunc("/repos/o/r/git/ref/heads/b", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Error in METHOD. Expected: GET, Got: %s", r.Method)
		}

		fmt.Fprint(w, `
			  {
				"ref": "refs/heads/b",
				"url": "https://api.github.com/repos/o/r/git/refs/heads/b",
				"object": {
				  "type": "commit",
				  "sha": "base-commit-sha-1",
				  "url": "https://api.github.com/repos/o/r/git/commits/new-commit-sha-1"
				}
			  }`)
	})

	// CreateRef() handling
	mux.HandleFunc("/repos/o/r/git/refs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Error in METHOD. Expected: POST, Got: %s", r.Method)
		}

		createdRef := new(updateRefRequest)
		json.NewDecoder(r.Body).Decode(createdRef)
		if *createdRef.SHA != "base-commit-sha-1" {
			fmt.Fprintf(w, "Wrong create ref commit sha. Expected: base-commit-sha-1, Got: %s", *createdRef.SHA)
		}

		fmt.Fprint(w, `
			  {
				"ref": "refs/heads/newBranch",
				"url": "https://api.github.com/repos/o/r/git/refs/heads/newBranch",
				"object": {
				  "type": "commit",
				  "sha": "new-commit-sha-1",
				  "url": "https://api.github.com/repos/o/r/git/commits/new-commit-sha-1"
				}
			  }`)
	})

	ctx := context.Background()
	handler := &GitHandler{
		ctx:         ctx,
		client:      client,
		user:        "o",
		repo:        "r",
		baseBranch:  "b",
		path:        path,
		commitUser:  author,
		commitEmail: email,
	}

	createdRef, err := handler.createNewBranch("newBranch")
	if err != nil {
		t.Errorf("Git Handler createNewBranch returned error: %v", err)
	}

	wantRef := &github.Reference{
		Ref: String("refs/heads/newBranch"),
		URL: String("https://api.github.com/repos/o/r/git/refs/heads/newBranch"),
		Object: &github.GitObject{
			Type: String("commit"),
			SHA:  String("new-commit-sha-1"),
			URL:  String("https://api.github.com/repos/o/r/git/commits/new-commit-sha-1"),
		},
	}

	if !cmp.Equal(createdRef, wantRef) {
		t.Errorf("Git.CreateTree returned %+v, want %+v", createdRef, wantRef)
	}

}

func TestGitHandler_NewPR(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	author := "a"
	email := "e"
	path := "p"

	expectedReceivedPR := &github.NewPullRequest{
		Title: String("Turbonomic Action: update yaml file p for resource test-cluster-id/test-ns/test-res"),
		Head:  String("test-branch"),
		Base:  String("b"),
		Body: String("This PR is automatically created via `Turbonomic action execution` \n\n" +
			"This PR intends to update pod template resources or replicas of resource `test-res`"),
		MaintainerCanModify: Bool(true),
	}

	mux.HandleFunc("/repos/o/r/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Error in METHOD. Expected: POST, Got: %s", r.Method)
		}

		receivedPR := new(github.NewPullRequest)
		json.NewDecoder(r.Body).Decode(receivedPR)
		if !cmp.Equal(receivedPR, expectedReceivedPR) {
			t.Errorf("Expected PR and newPR did not match got %+v, want %+v", receivedPR, expectedReceivedPR)
		}

		fmt.Fprint(w, `{"number":1}`)
	})

	ctx := context.Background()
	handler := &GitHandler{
		ctx:         ctx,
		client:      client,
		user:        "o",
		repo:        "r",
		baseBranch:  "b",
		path:        path,
		commitUser:  author,
		commitEmail: email,
	}

	expectedCreatedPR := &github.PullRequest{
		Number: Int(1),
	}

	createdPR, err := handler.newPR("test-res", "test-ns", "test-cluster-id", "test-branch")
	if err != nil {
		t.Errorf("Git Handler newPR returned error: %v", err)
	}

	if !cmp.Equal(expectedCreatedPR, createdPR) {
		t.Errorf("Created PR from newPR did not match got %+v, want %+v", createdPR, expectedCreatedPR)
	}
}

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
