package reaper

import (
	"context"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/logkey"
	"github.com/TeddyBertrand/Mayfly/internal/workflow"
)

// sweepStuck logs, once each, mayfly jobs queued for longer than StuckAfter, with a reason based on the owner probe.
func (r *Reaper) sweepStuck(ctx context.Context, repos []github.Repo, ownerErr error) []Stuck {
	var stuck []Stuck
	queued := map[int64]bool{}
	for _, repo := range repos {
		jobs, err := r.gh.QueuedJobs(ctx, repo.InstallationID, repo.FullName)
		if err != nil {
			r.logger.Warn("queued job list failed", logkey.Repo, repo.FullName, logkey.Err, err)
			for id := range r.reported {
				queued[id] = true
			}
			continue
		}
		for _, j := range jobs {
			if !workflow.MatchesLabels(j.Labels, r.opts.Labels) {
				continue
			}
			queued[j.ID] = true
			age := r.now().Sub(j.CreatedAt)
			if age < r.opts.StuckAfter || r.reported[j.ID] {
				continue
			}
			reason := ReasonNotPickedUp
			if ownerErr != nil {
				reason = ReasonMayflyUnreachable
			}
			r.reported[j.ID] = true
			r.metrics.StuckJobs.WithLabelValues(reasonLabels[reason]).Inc()
			r.logger.Warn("job stuck in queued", logkey.Repo, repo.FullName, logkey.JobID, j.ID, logkey.JobName, j.Name,
				logkey.Age, age.Round(time.Second).String(), logkey.Reason, reason, logkey.URL, j.URL)
			stuck = append(stuck, Stuck{Repo: repo.FullName, Job: j, Reason: reason})
		}
	}
	for id := range r.reported {
		if !queued[id] {
			delete(r.reported, id)
		}
	}
	return stuck
}
