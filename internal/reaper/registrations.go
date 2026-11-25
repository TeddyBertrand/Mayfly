package reaper

import (
	"context"
	"strings"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/logkey"
)

// sweepRegistrations deletes mayfly registrations that stayed offline, with no container claiming them,
// for StaleAfter. claimed holds the runner IDs of containers still alive after the container pass.
func (r *Reaper) sweepRegistrations(ctx context.Context, repos []github.Repo, claimed map[int64]bool) []github.RunnerRegistration {
	var deleted []github.RunnerRegistration
	seen := map[int64]bool{}
	for _, repo := range repos {
		regs, err := r.gh.Runners(ctx, repo.InstallationID, repo.FullName)
		if err != nil {
			r.logger.Warn("runner list failed, registrations kept", logkey.Repo, repo.FullName, logkey.Err, err)
			// Keep this repo's timers: a failed list says nothing about whether its runners recovered.
			for id := range r.staleSince {
				seen[id] = true
			}
			continue
		}
		for _, reg := range regs {
			// Only names the provisioner generates; other self-hosted runners on the repo are never touched.
			if !strings.HasPrefix(reg.Name, github.RunnerNamePrefix) || reg.Status != github.RunnerStatusOffline || claimed[reg.ID] {
				continue
			}
			seen[reg.ID] = true
			since, ok := r.staleSince[reg.ID]
			if !ok {
				r.staleSince[reg.ID] = r.now()
				continue
			}
			if r.now().Sub(since) < r.opts.StaleAfter {
				continue
			}
			log := r.logger.With(logkey.Repo, repo.FullName, logkey.RunnerID, reg.ID, logkey.RunnerName, reg.Name)
			if err := r.gh.DeleteRunner(ctx, repo.InstallationID, repo.FullName, reg.ID); err != nil {
				log.Error("stale registration delete failed", logkey.Err, err)
				continue
			}
			r.metrics.RegistrationsRemoved.Inc()
			log.Info("stale registration removed", logkey.StaleFor, r.now().Sub(since).Round(time.Second).String())
			delete(r.staleSince, reg.ID)
			deleted = append(deleted, reg)
		}
	}
	// A registration that came online, got a container or disappeared starts its window over if it goes stale again.
	for id := range r.staleSince {
		if !seen[id] {
			delete(r.staleSince, id)
		}
	}
	return deleted
}
