package gitops

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v42/github"
	gitlab "github.com/xanzy/go-gitlab"
)

const (
	baseURLPath = "/api-v3"
)

// githubTestSetup sets up a test HTTP server along with a github.Client that is
// configured to talk to that test server. Tests should register handlers on
// mux which provide mock responses for the API method being tested.
func githubTestSetup() (client *github.Client, mux *http.ServeMux, serverURL string, teardown func()) {
	// mux is the HTTP request multiplexer used with the test server.
	mux = http.NewServeMux()

	apiHandler := http.NewServeMux()
	apiHandler.Handle(baseURLPath+"/", http.StripPrefix(baseURLPath, mux))

	// server is a test HTTP server used to provide mock API responses.
	server := httptest.NewServer(apiHandler)

	// client is the GitHub client being tested and is
	// configured to use test server.
	client = github.NewClient(nil)
	url, _ := url.Parse(server.URL + baseURLPath + "/")
	client.BaseURL = url
	client.UploadURL = url

	return client, mux, server.URL, server.Close
}

// Bool is a helper routine that allocates a new bool value
// to store v and returns a pointer to it.
func Bool(v bool) *bool { return &v }

// Int is a helper routine that allocates a new int value
// to store v and returns a pointer to it.
func Int(v int) *int { return &v }

// Int64 is a helper routine that allocates a new int64 value
// to store v and returns a pointer to it.
func Int64(v int64) *int64 { return &v }

// String is a helper routine that allocates a new string value
// to store v and returns a pointer to it.
func String(v string) *string { return &v }

// gitlabTestSetup sets up a test HTTP server along with a gitlab.Client that is
// configured to talk to that test server.  Tests should register handlers on
// mux which provide mock responses for the API method being tested.
func gitlabTestSetup(t *testing.T) (*http.ServeMux, *httptest.Server, *gitlab.Client) {
	// mux is the HTTP request multiplexer used with the test server.
	mux := http.NewServeMux()

	// server is a test HTTP server used to provide mock API responses.
	server := httptest.NewServer(mux)

	// client is the Gitlab client being tested.
	client, err := gitlab.NewClient("", gitlab.WithBaseURL(server.URL))
	if err != nil {
		server.Close()
		t.Fatalf("Failed to create client: %v", err)
	}

	return mux, server, client
}

// gitlabTestTeardown closes the test HTTP server.
func gitlabTestTeardown(server *httptest.Server) {
	server.Close()
}

func testURL(t *testing.T, r *http.Request, want string) {
	if got := r.RequestURI; got != want {
		t.Errorf("Request url: %+v, want %s", got, want)
	}
}
