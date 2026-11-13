package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// perPage is GitHub's maximum page size; maxPages caps a listing so a misbehaving API cannot loop forever.
const (
	perPage  = 100
	maxPages = 10
)

// Repo is a repository the App is installed on.
type Repo struct {
	FullName       string
	InstallationID int64
}

// RunnerRegistration is one self-hosted runner registered on a repository.
type RunnerRegistration struct {
	ID     int64
	Name   string
	Status string
	Busy   bool
}

// QueuedJob is a workflow job waiting for a runner.
type QueuedJob struct {
	ID        int64
	RunID     int64
	Name      string
	Labels    []string
	CreatedAt time.Time
	URL       string
}

// list fetches every page of a GET listing at path and returns the items extracted from each page.
func list[T, R any](ctx context.Context, c *Client, path, bearer string, items func(R) []T) ([]T, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	var all []T
	for page := 1; ; page++ {
		var r R
		p := fmt.Sprintf("%s%sper_page=%d&page=%d", path, sep, perPage, page)
		if err := c.do(ctx, http.MethodGet, p, bearer, nil, &r, http.StatusOK); err != nil {
			return nil, err
		}
		got := items(r)
		all = append(all, got...)
		if len(got) < perPage || page >= maxPages {
			return all, nil
		}
	}
}

// Repos returns every repository of every installation of the App.
func (c *Client) Repos(ctx context.Context) ([]Repo, error) {
	jwt, err := appJWT(c.appID, c.key, c.now())
	if err != nil {
		return nil, err
	}
	type installation struct {
		ID int64 `json:"id"`
	}
	insts, err := list(ctx, c, "/app/installations", jwt, func(r []installation) []installation { return r })
	if err != nil {
		return nil, err
	}

	type reposPage struct {
		Repositories []struct {
			FullName string `json:"full_name"`
		} `json:"repositories"`
	}
	var repos []Repo
	for _, inst := range insts {
		token, err := c.InstallationToken(ctx, inst.ID)
		if err != nil {
			return nil, err
		}
		names, err := list(ctx, c, "/installation/repositories", token, func(r reposPage) []string {
			out := make([]string, 0, len(r.Repositories))
			for _, repo := range r.Repositories {
				out = append(out, repo.FullName)
			}
			return out
		})
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			repos = append(repos, Repo{FullName: n, InstallationID: inst.ID})
		}
	}
	return repos, nil
}

// Runners returns every self-hosted runner registered on repo.
func (c *Client) Runners(ctx context.Context, installationID int64, repo string) ([]RunnerRegistration, error) {
	if !repoPattern.MatchString(repo) {
		return nil, fmt.Errorf("list runners: invalid repo %q", repo)
	}
	token, err := c.InstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	type page struct {
		Runners []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
			Busy   bool   `json:"busy"`
		} `json:"runners"`
	}
	return list(ctx, c, "/repos/"+repo+"/actions/runners", token, func(p page) []RunnerRegistration {
		out := make([]RunnerRegistration, 0, len(p.Runners))
		for _, r := range p.Runners {
			out = append(out, RunnerRegistration{ID: r.ID, Name: r.Name, Status: r.Status, Busy: r.Busy})
		}
		return out
	})
}

// QueuedJobs returns the jobs of repo that are waiting for a runner, across all queued workflow runs.
func (c *Client) QueuedJobs(ctx context.Context, installationID int64, repo string) ([]QueuedJob, error) {
	if !repoPattern.MatchString(repo) {
		return nil, fmt.Errorf("list queued jobs: invalid repo %q", repo)
	}
	token, err := c.InstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	type runsPage struct {
		Runs []struct {
			ID int64 `json:"id"`
		} `json:"workflow_runs"`
	}
	runs, err := list(ctx, c, "/repos/"+repo+"/actions/runs?status=queued", token, func(p runsPage) []int64 {
		out := make([]int64, 0, len(p.Runs))
		for _, r := range p.Runs {
			out = append(out, r.ID)
		}
		return out
	})
	if err != nil {
		return nil, err
	}

	type job struct {
		ID        int64     `json:"id"`
		RunID     int64     `json:"run_id"`
		Name      string    `json:"name"`
		Status    string    `json:"status"`
		Labels    []string  `json:"labels"`
		CreatedAt time.Time `json:"created_at"`
		URL       string    `json:"html_url"`
	}
	type jobsPage struct {
		Jobs []job `json:"jobs"`
	}
	var jobs []QueuedJob
	for _, runID := range runs {
		all, err := list(ctx, c, fmt.Sprintf("/repos/%s/actions/runs/%d/jobs", repo, runID), token, func(p jobsPage) []job { return p.Jobs })
		if err != nil {
			return nil, err
		}
		for _, j := range all {
			// A queued run can hold jobs that already started or finished; only waiting ones count.
			if j.Status == "queued" {
				jobs = append(jobs, QueuedJob{ID: j.ID, RunID: j.RunID, Name: j.Name, Labels: j.Labels, CreatedAt: j.CreatedAt, URL: j.URL})
			}
		}
	}
	return jobs, nil
}
