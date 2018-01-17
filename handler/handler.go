// Package handler provides an http.Handler serving the endpoings of the github-reminder app.
package handler

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/google/go-github/github"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/src-d/github-reminder/reminder"
)

type server struct {
	appID     int
	key       []byte
	secret    []byte
	transport http.RoundTripper
}

// New returns a new http.Handler serving github-reminder endpoints.
// key should contain the app's private key for authentication.
// secret can be empty or contain the application's secret used for hook authentication.
// You can read more about secret's here: https://developer.github.com/webhooks/#delivery-headers.
func New(appID int, key, secret []byte, transport http.RoundTripper) (http.Handler, error) {
	if transport == nil {
		transport = http.DefaultTransport
	}

	s := &server{appID, key, secret, transport}
	r := mux.NewRouter()
	r.HandleFunc("/hook", s.hookHandler)
	r.HandleFunc("/cron", s.cronHandler)
	return r, nil
}

func (s *server) cronHandler(w http.ResponseWriter, r *http.Request) {
	client, err := reminder.NewApplicationClient(s.appID, s.key, s.transport)
	if err != nil {
		logrus.Errorf("could not create authenticated client: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	instIDs, err := client.Installations(ctx)
	if err != nil {
		logrus.Errorf("could not fetch installations: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	for _, instID := range instIDs {
		client, err := reminder.NewInstallationClient(s.appID, instID, s.key, s.transport)
		if err != nil {
			logrus.Errorf("could not create authenticated client: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if err = client.UpdateInstallation(ctx); err != nil {
			logrus.Errorf("could not update installation: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
	}
}

func (s *server) hookHandler(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		logrus.Warnf("could not read body: %v", err)
		http.Error(w, "could not read body", http.StatusInternalServerError)
		return
	}

	if err := checkSignature(r.Header.Get("X-Hub-Signature"), body, s.secret); err != nil {
		logrus.Warnf("bad signature: %v", err)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	inst, owner, name, issue, err := extractIssueInfo(r.Header.Get("X-Github-Event"), body)
	if err != nil {
		logrus.Warnf("could not extract issue info: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	client, err := reminder.NewInstallationClient(s.appID, inst, s.key, s.transport)
	if err != nil {
		logrus.Errorf("could not create authenticated client: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	err = client.UpdateIssue(r.Context(), owner, name, issue)
	if err != nil {
		logrus.Errorf("could not update issue: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

func extractIssueInfo(kind string, body []byte) (inst int, owner, name string, issue int, err error) {
	switch kind {
	case "issue_comment":
		var data github.IssueCommentEvent
		if err := json.Unmarshal(body, &data); err != nil {
			return 0, "", "", 0, errors.Wrap(err, "could not decode issue comment event")
		}
		repo := data.GetIssue().GetRepository()
		if data.GetIssue().Repository == nil {
			repo = data.GetRepo()
		}
		return data.GetInstallation().GetID(),
			repo.GetOwner().GetLogin(),
			repo.GetName(),
			data.GetIssue().GetNumber(),
			nil
	case "issues":
		var data github.IssuesEvent
		if err := json.Unmarshal(body, &data); err != nil {
			return 0, "", "", 0, errors.Wrap(err, "could not decode issue event")
		}
		return data.GetInstallation().GetID(),
			data.GetIssue().GetRepository().GetOwner().GetLogin(),
			data.GetIssue().GetRepository().GetName(),
			data.GetIssue().GetNumber(),
			nil
	case "pull_request":
		var data github.PullRequestEvent
		if err := json.Unmarshal(body, &data); err != nil {
			return 0, "", "", 0, errors.Wrap(err, "could not decode pull request event")
		}
		return data.GetInstallation().GetID(),
			data.GetPullRequest().GetHead().GetRepo().GetOwner().GetLogin(),
			data.GetPullRequest().GetHead().GetRepo().GetName(),
			data.GetPullRequest().GetNumber(),
			nil
	}
	return 0, "", "", 0, errors.Errorf("unkown event type %s", kind)
}

func checkSignature(got string, body, secret []byte) error {
	if secret == nil {
		return nil
	}

	if !strings.HasPrefix(got, "sha1=") {
		return errors.Errorf("unknown hashing algorithm")
	}
	got = got[5:]
	mac := hmac.New(sha1.New, secret)
	mac.Write(body)
	if wants := hex.EncodeToString(mac.Sum(nil)); wants != got {
		return errors.Errorf("wrong signature")
	}
	return nil
}
