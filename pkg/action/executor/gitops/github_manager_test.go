package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/v42/github"
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
			t.Errorf("Error in METHOD. Expected: POST, Got: %s", r.Method)
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
			t.Errorf("Error in METHOD. Expected: POST, Got: %s", r.Method)
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
