package reminder

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeClient satisfies the client interface.
type fakeClient struct {
	_installations      func(ctx context.Context) ([]int, error)
	_repos              func(ctx context.Context) ([]repository, error)
	_repoLabels         func(ctx context.Context, owner, repo string) ([]string, error)
	_issues             func(ctx context.Context, owner, repo string) ([]int, error)
	_issue              func(ctx context.Context, owner, repo string, number int) (*issue, error)
	_createIssueComment func(ctx context.Context, owner, repo string, number int, body string) error
	_removeIssueLabel   func(ctx context.Context, owner, repo string, number int, label string) error
	_addIssueLabel      func(ctx context.Context, owner, repo string, number int, label string) error
}

func (f *fakeClient) installations(ctx context.Context) ([]int, error) {
	return f._installations(ctx)
}
func (f *fakeClient) repos(ctx context.Context) ([]repository, error) {
	return f._repos(ctx)
}
func (f *fakeClient) repoLabels(ctx context.Context, owner, repo string) ([]string, error) {
	return f._repoLabels(ctx, owner, repo)
}
func (f *fakeClient) issues(ctx context.Context, owner, repo string) ([]int, error) {
	return f._issues(ctx, owner, repo)
}
func (f *fakeClient) issue(ctx context.Context, owner, repo string, number int) (*issue, error) {
	return f._issue(ctx, owner, repo, number)
}
func (f *fakeClient) createIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	return f._createIssueComment(ctx, owner, repo, number, body)
}
func (f *fakeClient) removeIssueLabel(ctx context.Context, owner, repo string, number int, label string) error {
	return f._removeIssueLabel(ctx, owner, repo, number, label)
}
func (f *fakeClient) addIssueLabel(ctx context.Context, owner, repo string, number int, label string) error {
	return f._addIssueLabel(ctx, owner, repo, number, label)
}

func TestInstallations(t *testing.T) {
	ac := ApplicationClient{42, &fakeClient{
		_installations: func(context.Context) ([]int, error) { return []int{100}, nil },
	}}
	ids, err := ac.Installations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 1 || ids[0] != 100 {
		t.Errorf("expected installation list to have only 100, got %v", ids)
	}
}

func TestAddFirstReminderComment(t *testing.T) {
	called := 0
	ic := InstallationClient{42, 43, &fakeClient{
		_repoLabels: func(ctx context.Context, owner, repo string) ([]string, error) { return nil, nil },
		_issue: func(ctx context.Context, owner, repo string, number int) (*issue, error) {
			return &issue{
				repo:   repository{owner, repo},
				number: number,
				title:  "Test",
				body:   "Nothing to be seen here",
				author: "deadline-reminder[bot]",
				state:  "open",
				comments: []comment{{
					author: "francesc",
					body:   fmt.Sprintf("reminder: %s\n", time.Now().Format("2006-01-02")),
				}},
			}, nil
		},
		_createIssueComment: func(ctx context.Context, owner, repo string, number int, body string) error {
			called++
			if !strings.Contains(body, "francesc") {
				return fmt.Errorf("missing username in the issue body: %s", body)
			}
			return nil
		},
	}}

	if err := ic.UpdateIssue(context.Background(), "foo", "bar", 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called == 0 {
		t.Fatalf("no comment was created, expected a new one")
	}
	if called > 1 {
		t.Fatalf("expected only one comment created; got %d", called)
	}
}

func TestAvoidAddingSecondReminderComment(t *testing.T) {
	ic := InstallationClient{42, 43, &fakeClient{
		_repoLabels: func(ctx context.Context, owner, repo string) ([]string, error) { return nil, nil },
		_issue: func(ctx context.Context, owner, repo string, number int) (*issue, error) {
			return &issue{
				repo:   repository{owner, repo},
				number: number,
				title:  "Test",
				body:   "Nothing to see here",
				author: "francesc",
				state:  "open",
				comments: []comment{{
					author:  "francesc",
					body:    fmt.Sprintf("reminder: %s\n", time.Now().Format("2006-01-02")),
					created: time.Now().Add(-96 * time.Hour),
				}, {
					author:  "deadline-reminder[bot]",
					body:    "hi @francesc, it's reminder time!",
					created: time.Now().Add(-24 * time.Hour),
				}},
			}, nil
		},
		_createIssueComment: func(ctx context.Context, owner, repo string, number int, body string) error {
			return fmt.Errorf("expected no new comments; got %s", body)
		},
	}}

	if err := ic.UpdateIssue(context.Background(), "foo", "bar", 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
