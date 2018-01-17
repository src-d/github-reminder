// Package reminder provides the clients used by the github-reminder app.
package reminder

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/github"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// An ApplicationClient provides the methods that do not depend on an installation.
type ApplicationClient struct {
	appID  int
	client *github.Client
}

// NewApplicationClient returns a new ApplicationClient.
// If the given transport is nil, http.DefaultTransport will be used instead.
func NewApplicationClient(appID int, key []byte, transport http.RoundTripper) (*ApplicationClient, error) {
	if transport == nil {
		transport = http.DefaultTransport
	}

	itr, err := ghinstallation.NewAppsTransport(transport, appID, key)
	if err != nil {
		return nil, errors.Wrap(err, "could not create authenticated application client")
	}
	return &ApplicationClient{
		appID:  appID,
		client: github.NewClient(&http.Client{Transport: itr}),
	}, nil
}

// Installations lists all of the installation ids for the authenticated application.
func (c *ApplicationClient) Installations(ctx context.Context) ([]int, error) {
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

// An InstallationClient provides all of the features depending on a specific installation.
type InstallationClient struct {
	appID          int
	installationID int
	client         *github.Client
}

// NewInstallationClient returns a new InstallationClient.
// If transport is nil http.DefaultTransport will be used.
func NewInstallationClient(appID, installationID int, key []byte, transport http.RoundTripper) (*InstallationClient, error) {
	itr, err := ghinstallation.New(transport, appID, installationID, key)
	if err != nil {
		return nil, errors.Wrap(err, "could not created authenticated installation client")
	}
	return &InstallationClient{
		appID:          appID,
		installationID: installationID,
		client:         github.NewClient(&http.Client{Transport: itr}),
	}, nil
}

// UpdateInstallation iterates over all of the repositories in the installation updating all deadline labels.
func (c *InstallationClient) UpdateInstallation(ctx context.Context) error {
	logrus.Infof("updating all repos for installation %d/%d", c.appID, c.installationID)

	repos, _, err := c.client.Apps.ListRepos(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "could not list repositories")
	}

	for _, repo := range repos {
		if err := c.UpdateRepo(ctx, repo.GetOwner().GetLogin(), repo.GetName()); err != nil {
			return errors.Wrapf(err, "could not handle repository %s", repo.GetFullName())
		}
	}
	return nil
}

// UpdateRepo iterates over all of the issues and PRs in a repository updating all deadline labels.
func (c *InstallationClient) UpdateRepo(ctx context.Context, owner, name string) error {
	logrus.Debugf("handling repository %s/%s", owner, name)

	labels, err := c.LabelsInRepo(ctx, owner, name)
	if err != nil {
		return err
	}
	if len(labels) == 0 {
		return nil
	}

	for i, l := range labels {
		logrus.Debugf("label #%d: %s", i, l.Name)
	}

	issues, _, err := c.client.Issues.ListByRepo(ctx, owner, name, &github.IssueListByRepoOptions{State: "open"})
	if err != nil {
		return errors.Wrap(err, "could not list issues")
	}

	for _, issue := range issues {
		if err := c.updateIssue(ctx, owner, name, labels, issue); err != nil {
			return errors.Wrapf(err, "could not handle issue %d", issue.GetNumber())
		}

	}

	return nil
}

// A Label has simply a name and the corresponding number of days.
type Label struct {
	Name string
	Days int
}

// LabelsInRepo lists all of the deadline related labels in a repository.
func (c *InstallationClient) LabelsInRepo(ctx context.Context, owner, name string) ([]Label, error) {
	ls, _, err := c.client.Issues.ListLabels(ctx, owner, name, nil)
	if err != nil {
		return nil, errors.Wrap(err, "could not list labels")
	}

	var labels []Label

	const prefix = "deadline < "
	for _, l := range ls {
		name := l.GetName()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		days, err := strconv.Atoi(strings.TrimPrefix(name, prefix))
		if err != nil {
			logrus.Errorf("could not parse days in %s", name)
			continue
		}
		labels = append(labels, Label{name, days})
	}

	sort.Slice(labels, func(i, j int) bool { return labels[i].Days < labels[j].Days })
	return labels, nil
}

// UpdateIssue finds a deadline in the issue and updates its labels accordingly.
func (c *InstallationClient) UpdateIssue(ctx context.Context, owner, name string, number int) error {
	labels, err := c.LabelsInRepo(ctx, owner, name)
	if err != nil {
		return err
	}

	issue, _, err := c.client.Issues.Get(ctx, owner, name, number)
	if err != nil {
		return errors.Wrapf(err, "could not fetch issue %s/%s#%d", owner, name, number)
	}

	return c.updateIssue(ctx, owner, name, labels, issue)
}

func (c *InstallationClient) updateIssue(ctx context.Context, owner, name string, labels []Label, issue *github.Issue) error {
	logrus.Debugf("handling issue #%d: %s", issue.GetNumber(), issue.GetTitle())
	if issue.GetState() != "open" {
		return nil
	}

	comments, _, err := c.client.Issues.ListComments(ctx, owner, name, issue.GetNumber(), nil)
	if err != nil {
		return errors.Wrap(err, "could not fetch comments")
	}

	deadline := findDeadline(issue.GetBody())
	for _, comment := range comments {
		if d := findDeadline(comment.GetBody()); !d.IsZero() {
			deadline = d
		}
	}

	days := time.Until(deadline).Hours() / 24

	logrus.Debugf("issue #%d deadline in %v days", issue.GetNumber(), days)
	var labelIdx int
	for labelIdx = 0; labelIdx < len(labels); labelIdx++ {
		if labels[labelIdx].Days > int(days) {
			break
		}
	}

	for i, l := range labels {
		if i != labelIdx {
			c.client.Issues.RemoveLabelForIssue(ctx, owner, name, issue.GetNumber(), l.Name)
		}
	}

	// new deadline is too large for labels.
	if labelIdx >= len(labels) {
		return nil
	}

	newLabel := labels[labelIdx]
	logrus.Debugf("applying %s to issue#%d", newLabel.Name, issue.GetNumber())
	_, _, err = c.client.Issues.AddLabelsToIssue(ctx, owner, name, issue.GetNumber(), []string{newLabel.Name})
	return errors.Wrapf(err, "could not apply label %s", newLabel.Name)
}

const triggerWord = "deadline"

func findDeadline(s string) time.Time {
	var deadline time.Time
	s = strings.ToLower(s)
	for {
		i := strings.Index(s, triggerWord)
		if i < 0 {
			return deadline
		}
		s = s[i+len(triggerWord):]

		lb := strings.Index(s, "\n")
		if lb < 0 {
			lb = len(s)
		}

		if d := parseDeadline(s[:lb]); !d.IsZero() {
			deadline = d
		}
	}
}

var dateLayouts = []string{
	"2006/01/02",
	"2006 January 2",
	"2006 Jan 2",
	"January 2 2006",
	"Jan 2 2006",
}

func parseDeadline(s string) time.Time {
	s = strings.TrimSpace(strings.Trim(strings.TrimSpace(s), ":"))
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
