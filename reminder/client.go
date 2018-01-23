package reminder

import (
	"context"
	"time"

	"github.com/google/go-github/github"
	"github.com/pkg/errors"
)

type repository struct {
	owner string
	name  string
}

type comment struct {
	author  string
	body    string
	created time.Time
}

type issue struct {
	repo     repository
	number   int
	title    string
	body     string
	author   string
	state    string
	comments []comment
}

type client interface {
	installations(ctx context.Context) ([]int, error)
	repos(ctx context.Context) ([]repository, error)
	repoLabels(ctx context.Context, owner, repo string) ([]string, error)
	issues(ctx context.Context, owner, repo string) ([]int, error)
	issue(ctx context.Context, owner, repo string, number int) (*issue, error)
	createIssueComment(ctx context.Context, owner, repo string, number int, body string) error
	removeIssueLabel(ctx context.Context, owner, repo string, number int, label string) error
	addIssueLabel(ctx context.Context, owner, repo string, number int, label string) error
}

type githubClient struct{ client *github.Client }

func (c *githubClient) installations(ctx context.Context) ([]int, error) {
	insts, _, err := c.client.Apps.ListInstallations(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "could not fetch installations")
	}

	var ids []int
	for _, inst := range insts {
		ids = append(ids, inst.GetID())
	}
	return ids, nil
}

func (c *githubClient) repos(ctx context.Context) ([]repository, error) {
	rs, _, err := c.client.Apps.ListRepos(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "could not list repositories")
	}

	res := make([]repository, 0, len(rs))
	for _, r := range rs {
		res = append(res, repository{r.GetOwner().GetLogin(), r.GetName()})
	}
	return res, nil
}

func (c *githubClient) repoLabels(ctx context.Context, owner, repo string) ([]string, error) {
	ls, _, err := c.client.Issues.ListLabels(ctx, owner, repo, nil)
	if err != nil {
		return nil, errors.Wrap(err, "could not list labels")
	}
	ss := make([]string, 0, len(ls))
	for _, l := range ls {
		ss = append(ss, l.GetName())
	}
	return ss, nil
}

func (c *githubClient) issues(ctx context.Context, owner, repo string) ([]int, error) {
	issues, _, err := c.client.Issues.ListByRepo(ctx, owner, repo, &github.IssueListByRepoOptions{State: "open"})
	if err != nil {
		return nil, errors.Wrap(err, "could not list issues")
	}

	ids := make([]int, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.GetNumber())
	}
	return ids, nil
}

func (c *githubClient) issue(ctx context.Context, owner, repo string, number int) (*issue, error) {
	res, _, err := c.client.Issues.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, errors.Wrapf(err, "could not fetch issue %s/%s#%d", owner, repo, number)
	}

	cs, _, err := c.client.Issues.ListComments(ctx, owner, repo, number, nil)
	if err != nil {
		return nil, errors.Wrap(err, "could not fetch comments")
	}

	i := &issue{
		repo:   repository{owner, repo},
		number: number,
		title:  res.GetTitle(),
		body:   res.GetBody(),
		author: res.GetUser().GetLogin(),
		state:  res.GetState(),
	}
	for _, c := range cs {
		i.comments = append(i.comments, comment{
			author:  c.GetUser().GetLogin(),
			body:    c.GetBody(),
			created: c.GetCreatedAt(),
		})
	}
	return i, nil
}

func (c *githubClient) createIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	_, _, err := c.client.Issues.CreateComment(ctx, owner, repo, number, &github.IssueComment{Body: &body})
	return err
}

func (c *githubClient) removeIssueLabel(ctx context.Context, owner, repo string, number int, label string) error {
	_, err := c.client.Issues.RemoveLabelForIssue(ctx, owner, repo, number, label)
	return err
}

func (c *githubClient) addIssueLabel(ctx context.Context, owner, repo string, number int, label string) error {
	_, _, err := c.client.Issues.AddLabelsToIssue(ctx, owner, repo, number, []string{label})
	return err
}
