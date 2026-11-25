package reaper

import (
	"context"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/logkey"
	"github.com/TeddyBertrand/Mayfly/internal/metrics"
	"github.com/TeddyBertrand/Mayfly/internal/worker"
)

// Reasons a worker is removed, as logged and returned by Sweep.
const (
	ReasonMaxAge           = "max age exceeded"
	ReasonOwnerGone        = "owner instance gone"
	ReasonOwnerUnreachable = "owner unreachable"
	ReasonRunnerGone       = "runner registration gone"
	ReasonRunnerOffline    = "runner offline"

	// ReasonMayflyUnreachable and ReasonNotPickedUp explain a stuck job.
	ReasonMayflyUnreachable = "mayfly unreachable"
	ReasonNotPickedUp       = "not picked up: mayfly is up but no runner took the job, check its logs for this job"
)

// reasonLabels are the short reason label values on the reaper metrics, one per reason above.
var reasonLabels = map[string]string{
	ReasonMaxAge:            "max_age",
	ReasonOwnerGone:         "owner_gone",
	ReasonOwnerUnreachable:  "owner_unreachable",
	ReasonRunnerGone:        "runner_gone",
	ReasonRunnerOffline:     "runner_offline",
	ReasonMayflyUnreachable: "mayfly_unreachable",
	ReasonNotPickedUp:       "not_picked_up",
}

// presetReasons starts every reason series at 0, so increase() also counts the first removal or stuck job of each kind.
func presetReasons(m *metrics.Reaper) {
	for _, r := range []string{ReasonMaxAge, ReasonOwnerGone, ReasonOwnerUnreachable, ReasonRunnerGone, ReasonRunnerOffline} {
		metrics.Preset(m.WorkersRemoved, reasonLabels[r])
	}
	for _, r := range []string{ReasonMayflyUnreachable, ReasonNotPickedUp} {
		metrics.Preset(m.StuckJobs, reasonLabels[r])
	}
}

// reason returns why w should be removed, or "" to keep it. live and ownerErr are this cycle's owner probe.
func (r *Reaper) reason(ctx context.Context, w worker.Info, live string, ownerErr error) string {
	age := r.now().Sub(w.Created)
	switch {
	case age > r.opts.MaxAge:
		return ReasonMaxAge
	case ownerErr == nil && w.Owner != live:
		return ReasonOwnerGone
	case ownerErr != nil && r.missed >= r.opts.MissedProbes:
		return ReasonOwnerUnreachable
	}
	if age < r.opts.Grace || w.RunnerID == 0 || w.Repo == "" {
		return ""
	}
	runner, err := r.gh.Runner(ctx, w.InstallationID, w.Repo, w.RunnerID)
	switch {
	case err != nil:
		r.logger.Warn("runner lookup failed, worker kept", logkey.ContainerID, worker.ShortID(w.ID), logkey.Err, err)
		return ""
	case !runner.Found:
		return ReasonRunnerGone
	case runner.Status == github.RunnerStatusOffline:
		return ReasonRunnerOffline
	}
	return ""
}
