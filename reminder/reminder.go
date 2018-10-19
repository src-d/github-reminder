// Package reminder provides the clients used by the github-reminder app.
package reminder

import (
	"context"
	"fmt"
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

// newClient can be replaced by test cases.
var newClient = func(client *http.Client) client {
	return &githubClient{github.NewClient(client)}
}

// An ApplicationClient provides the methods that do not depend on an installation.
type ApplicationClient struct {
	appID  int
	client client
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
		client: newClient(&http.Client{Transport: itr}),
	}, nil
}

// Installations lists all of the installation ids for the authenticated application.
func (c *ApplicationClient) Installations(ctx context.Context) ([]int, error) {
	return c.client.installations(ctx)
}

// An InstallationClient provides all of the features depending on a specific installation.
type InstallationClient struct {
	appID          int
	installationID int
	client         client
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
		client:         newClient(&http.Client{Transport: itr}),
	}, nil
}

// UpdateInstallation iterates over all of the repositories in the installation updating all deadline labels.
func (c *InstallationClient) UpdateInstallation(ctx context.Context) error {
	logrus.Infof("updating all repos for installation %d/%d", c.appID, c.installationID)

	repos, err := c.client.repos(ctx)
	if err != nil {
		return errors.Wrap(err, "could not list repositories")
	}

	for _, repo := range repos {
		if err := c.UpdateRepo(ctx, repo.owner, repo.name); err != nil {
			return errors.Wrapf(err, "could not handle repository %s/%s", repo.owner, repo.name)
		}
	}
	return nil
}

// UpdateRepo iterates over all of the issues and PRs in a repository updating all deadline labels.
func (c *InstallationClient) UpdateRepo(ctx context.Context, owner, repo string) error {
	logrus.Debugf("handling repository %s/%s", owner, repo)

	labels, err := c.LabelsInRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	if len(labels) == 0 {
		return nil
	}

	for i, l := range labels {
		logrus.Debugf("label #%d: %s", i, l.Name)
	}

	numbers, err := c.client.issues(ctx, owner, repo)
	if err != nil {
		return errors.Wrap(err, "could not list issues")
	}

	for _, number := range numbers {
		if err := c.updateIssue(ctx, owner, repo, number, labels); err != nil {
			return errors.Wrapf(err, "could not handle issue %d", number)
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
func (c *InstallationClient) LabelsInRepo(ctx context.Context, owner, repo string) ([]Label, error) {
	labels, err := c.client.repoLabels(ctx, owner, repo)
	if err != nil {
		return nil, errors.Wrap(err, "could not list labels")
	}

	var list []Label

	const prefix = "deadline < "
	for _, label := range labels {
		if !strings.HasPrefix(label, prefix) {
			continue
		}
		days, err := strconv.Atoi(strings.TrimPrefix(label, prefix))
		if err != nil {
			logrus.Errorf("could not parse days in %s", label)
			continue
		}
		list = append(list, Label{label, days})
	}

	sort.Slice(list, func(i, j int) bool { return list[i].Days < list[j].Days })
	return list, nil
}

// UpdateIssue finds a deadline in the issue and updates its labels accordingly.
func (c *InstallationClient) UpdateIssue(ctx context.Context, owner, repo string, number int) error {
	labels, err := c.LabelsInRepo(ctx, owner, repo)
	if err != nil {
		return err
	}

	return c.updateIssue(ctx, owner, repo, number, labels)
}

func (c *InstallationClient) updateIssue(ctx context.Context, owner, repo string, number int, labels []Label) error {
	logrus.Debugf("handling issue %s/%s#%d", owner, repo, number)
	issue, err := c.client.issue(ctx, owner, repo, number)
	if err != nil {
		return err
	}
	if issue.state != "open" {
		return nil
	}

	if err = c.checkReminders(ctx, issue); err != nil {
		return err
	}

	bodies := []string{issue.body}
	for _, comment := range issue.comments {
		bodies = append(bodies, comment.body)
	}
	deadlines := findTimes("deadline", bodies...)
	if len(deadlines) == 0 {
		return nil
	}
	deadline := deadlines[len(deadlines)-1]
	return c.checkDeadlines(ctx, owner, repo, number, deadline, labels)
}

func (c *InstallationClient) checkReminders(ctx context.Context, issue *issue) error {
	var reminded []time.Time
	for _, comment := range issue.comments {
		if comment.author == "deadline-reminder[bot]" {
			date := comment.created
			date = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
			reminded = append(reminded, date)
		}
	}

	check := func(author, body string) error {
		for _, reminder := range findTimes("reminder", body) {
			now := time.Now()
			// not the same day
			if reminder.Day() != now.Day() || reminder.Month() != now.Month() || reminder.Year() != now.Year() {
				continue
			}

			done := false
			for _, r := range reminded {
				done = done || r.Equal(reminder)
			}
			if done {
				continue
			}

			text := fmt.Sprintf("hi @%s, it's reminder day!", author)
			err := c.client.createIssueComment(ctx, issue.repo.owner, issue.repo.name, issue.number, text)
			if err != nil {
				return errors.Wrapf(err, "could not comment on %s/%s#%d", issue.repo.owner, issue.repo.name, issue.number)
			}
		}
		return nil
	}

	if err := check(issue.author, issue.body); err != nil {
		return err
	}
	for _, comment := range issue.comments {
		if err := check(comment.author, comment.body); err != nil {
			return err
		}
	}
	return nil
}

func (c *InstallationClient) checkDeadlines(ctx context.Context, owner, repo string, number int, deadline time.Time, labels []Label) error {
	days := time.Until(deadline).Hours() / 24

	logrus.Debugf("issue #%d deadline in %v days", number, days)
	labelIdx := -1
	if days > -1 {
		for labelIdx = 0; labelIdx < len(labels); labelIdx++ {
			if labels[labelIdx].Days > int(days) {
				break
			}
		}
	}

	for i, l := range labels {
		if i == labelIdx {
			continue
		}
		c.client.removeIssueLabel(ctx, owner, repo, number, l.Name)
	}

	// new deadline is too large for labels.
	if labelIdx >= len(labels) {
		return nil
	}
	// deadline is in the past.
	if labelIdx < 0 {
		return nil
	}

	newLabel := labels[labelIdx]
	logrus.Debugf("applying %s to issue %s/%s#%d", newLabel.Name, owner, repo, number)
	err := c.client.addIssueLabel(ctx, owner, repo, number, newLabel.Name)
	return errors.Wrapf(err, "could not apply label %s", newLabel.Name)
}

func findTimes(word string, bodies ...string) []time.Time {
	var times []time.Time
	for _, body := range bodies {
		body = strings.ToLower(body)
		for {
			i := strings.Index(body, word)
			if i < 0 {
				break
			}
			body = body[i+len(word):]

			lb := strings.Index(body, "\n")
			if lb < 0 {
				lb = len(body)
			}

			if d := parseDate(body[:lb]); !d.IsZero() {
				times = append(times, d)
			}
		}
	}

	return times
}

var dateLayouts = []string{
	"2006/01/02",
	"2006-01-02",
	"2006 January 2",
	"2006 Jan 2",
	"January 2 2006",
	"Jan 2 2006",
	"January 2, 2006",
	"Jan 2, 2006",
}

func parseDate(s string) time.Time {
	s = strings.TrimSpace(strings.Trim(strings.TrimSpace(s), ":"))
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
