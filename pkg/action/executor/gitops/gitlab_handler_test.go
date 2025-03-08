package gitops

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestGetHeadBranch(t *testing.T) {
	mux, server, client := gitlabTestSetup(t)
	defer gitlabTestTeardown(server)

	mux.HandleFunc("/api/v4/projects/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":1, "default_branch": "master"}`)
	})

	ctx := context.Background()
	gitHandler := GitHandler{
		ctx:         ctx,
		user:        "o",
		repo:        "r",
		baseBranch:  "b",
		path:        "p",
		commitUser:  "a",
		commitEmail: "e",
	}
	handler := GitLabHandler{
		GitHandler: gitHandler,
		client:     client,
	}

	wantBranch := "master"
	gotBranch, err := handler.getHeadBranch()
	if err != nil || gotBranch != wantBranch {
		t.Errorf("gitlab handler getHeadBranch error: (%v) got branch (%+v), want branch (%+v)",
			err, gotBranch, wantBranch)
	}
}

func TestGetRemoteFileContent(t *testing.T) {
	mux, server, client := gitlabTestSetup(t)
	defer gitlabTestTeardown(server)

	mux.HandleFunc("/api/v4/projects/", func(w http.ResponseWriter, r *http.Request) {
		// For some reason the multiplexer is not able to call the handler parsing
		// the whole url, that is why we install it on "/api/v4/projects/"
		// and then multiplex within one handler
		if strings.Contains(r.RequestURI, "/api/v4/projects/o%2Fr/repository/tree") {
			fmt.Fprintf(w, `
			[
			  {
				"id": "a1e8f8d745cc87e3a9248358d9352bb7f9a0aeba",
				"name": "html",
				"type": "tree",
				"path": "files/yaml",
				"mode": "040000"
			  },
			  {
				"id": "a1e8f8d745cc87e3a9248358d9352bb7f9a0aebb",
				"name": "html",
				"type": "tree",
				"path": "files/non-yaml",
				"mode": "040000"
			  },
			  {
				"id": "a1e8f8d745cc87e3a9248358d9352bb7f9a0aebc",
				"name": "html",
				"type": "blob",
				"path": "files/yaml/1.yaml",
				"mode": "040000"
			  }
			]
		`)
		} else if strings.Contains(r.RequestURI, "/api/v4/projects/o%2Fr/repository/files/files%2Fyaml%2F1%2Eyaml?ref=master") {
			fmt.Fprintf(w, `
			{
			  "file_name": "1.yaml",
			  "file_path": "files/yaml/1.yaml",
			  "encoding": "base64",
			  "content": "YXBpVmVyc2lvbjogYXBwcy92MQpraW5kOiBEZXBsb3ltZW50Cm1ldGFkYXRhOgogIGFubm90YXRpb25zOiBudWxsCiAgbGFiZWxzOgogICAgYXBwOiBiZWUKICBuYW1lOiBteS10ZXN0LW9iagpzcGVjOgogIHJlcGxpY2FzOiAxCiAgc2VsZWN0b3I6CiAgICBtYXRjaExhYmVsczoKICAgICAgYXBwOiBiZWUKICB0ZW1wbGF0ZToKICAgIG1ldGFkYXRhOgogICAgICBsYWJlbHM6CiAgICAgICAgYXBwOiBiZWUKICAgIHNwZWM6CiAgICAgIGltYWdlUHVsbFNlY3JldHM6CiAgICAgIC0gbmFtZTogZG9ja2VyLWNyZWRlbnRpYWxzCiAgICAgIGNvbnRhaW5lcnM6CiAgICAgIC0gbmFtZTogYmVla21hbi0xCiAgICAgICAgaW1hZ2U6IGJlZWttYW45NTI3L2NwdW1lbWxvYWQ6bGF0ZXN0CiAgICAgICAgZW52OgogICAgICAgIC0gbmFtZTogUlVOX1RZUEUKICAgICAgICAgIHZhbHVlOiBjcHUKICAgICAgICAtIG5hbWU6IENQVV9QRVJDRU5UCiAgICAgICAgICB2YWx1ZTogOTUKICAgICAgICByZXNvdXJjZXM6CiAgICAgICAgICByZXF1ZXN0czoKICAgICAgICAgICAgbWVtb3J5OiAxME1pCiAgICAgICAgICAgIGNwdTogMTBtCiAgICAgICAgICBsaW1pdHM6CiAgICAgICAgICAgIG1lbW9yeTogMjhNaQogICAgICAgICAgICBjcHU6IDExbQo=",
			  "content_sha256": "ba65759440c72a1d3e4c91ad06fbee879a7022217ba2be34e69f71cd67751bd6",
			  "ref": "master",
			  "blob_id": "79f7bbd25901e8334750839545a9bd021f0e4c83",
			  "commit_id": "d5a3ff139356ce33e37e73add446f16869741b50",
			  "last_commit_id": "570e7b2abdd848b95f2f578043fc23bd6f6fd24d"
			}
		`)
		}
	})

	ctx := context.Background()
	gitHandler := GitHandler{
		ctx:         ctx,
		user:        "o",
		repo:        "r",
		baseBranch:  "b",
		path:        "p",
		commitUser:  "a",
		commitEmail: "e",
	}
	handler := GitLabHandler{
		GitHandler: gitHandler,
		client:     client,
	}

	// The base 64 encoded content corresponds to exactly the below yaml
	// content true to the new lines and tabs/spaces
	wantContent := `apiVersion: apps/v1
kind: Deployment
metadata:
  annotations: null
  labels:
    app: bee
  name: my-test-obj
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
          value: cpu
        - name: CPU_PERCENT
          value: 95
        resources:
          requests:
            memory: 10Mi
            cpu: 10m
          limits:
            memory: 28Mi
            cpu: 11m
`
	gotContent, filePath, err := handler.getRemoteFileContent("my-test-obj", "files", "master")
	if err != nil {
		t.Errorf("gitlab handler getRemoteFileContent error : (%v)", err)
	}
	if strings.Compare(gotContent, wantContent) != 0 {
		t.Errorf("gitlab handler getRemoteFileContent got Content (%+v), want Content (%+v)",
			gotContent, wantContent)
	}
	if filePath != "files/yaml/1.yaml" {
		t.Errorf("gitlab handler getRemoteFileContent got wrong file path, expected "+
			"(files/yaml/1.yaml), got (%v).", filePath)
	}
}

func TestNewMR(t *testing.T) {
	mux, server, client := gitlabTestSetup(t)
	defer gitlabTestTeardown(server)

	mux.HandleFunc("/api/v4/projects/", func(w http.ResponseWriter, r *http.Request) {
		// For some reason the multiplexer is not able to call the handler parsing
		// the whole url, that is why we install it on "/api/v4/projects/"
		// and then multiplex within one handler
		if strings.Contains(r.RequestURI, "/api/v4/projects/o%2Fr/merge_requests") {
			fmt.Fprintf(w, `
			{
				"id": 12345678,
				"iid": 1,
				"project_id": 1,
				"title": "Turbonomic Action: update yaml file p for resource test-cluster-id/test-ns/test-res",
				"description": "This MR is automatically created via Turbonomic action execution \n\nThis MR intends to update pod template resources or replicas of resource test-res",
				"state": "opened",
				"target_branch": "target-branch",
				"source_branch": "source-branch",
				"author": {
				  "id": 12345,
				  "username": "o"
				}
			}
		`)
		}
	})

	ctx := context.Background()
	gitHandler := GitHandler{
		ctx:         ctx,
		user:        "o",
		repo:        "r",
		baseBranch:  "target-branch",
		path:        "p",
		commitUser:  "a",
		commitEmail: "e",
	}
	handler := GitLabHandler{
		GitHandler: gitHandler,
		client:     client,
	}

	resName := "test-res"
	resNamespace := "test-ns"
	clusterId := "test-cluster-id"
	newBranchName := "source-branch"
	title := fmt.Sprintf("Turbonomic Action: update yaml file %s for resource %s/%s/%s",
		handler.path, clusterId, resNamespace, resName)
	description := "This MR is automatically created via Turbonomic action execution \n\n" +
		"This MR intends to update pod template resources or replicas of resource " + resName

	author := gitlab.BasicUser{
		ID:       12345,
		Username: "o",
	}
	wantMr := gitlab.MergeRequest{
		ID:           12345678,
		IID:          1,
		State:        "opened",
		TargetBranch: gitHandler.baseBranch,
		SourceBranch: newBranchName,
		Title:        title,
		Description:  description,
		Author:       &author,
	}

	mr, err := handler.newMR(resName, resNamespace, clusterId, newBranchName)
	if err != nil {
		t.Errorf("Got error creating new MR :%v", err)
	}

	require.Equal(t, mr.ID, wantMr.ID)
	require.Equal(t, mr.IID, wantMr.IID)
	require.Equal(t, mr.State, wantMr.State)
	require.Equal(t, mr.SourceBranch, wantMr.SourceBranch)
	require.Equal(t, mr.TargetBranch, wantMr.TargetBranch)
	require.Equal(t, mr.Title, wantMr.Title)
	require.Equal(t, mr.Description, wantMr.Description)
	require.Equal(t, mr.Author.ID, wantMr.Author.ID)
	require.Equal(t, mr.Author.Username, wantMr.Author.Username)
}
