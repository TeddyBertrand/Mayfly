package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/worker"
)

// Inventory lists and removes worker containers, independently of the Backend that started them.
type Inventory struct {
	eng *engine
}

var _ worker.Inventory = (*Inventory)(nil)

// NewInventory returns an Inventory using the daemon on socket.
func NewInventory(socket string) *Inventory {
	return &Inventory{eng: newSocketEngine(socket)}
}

// List returns every container carrying the mayfly.job_id label, running or not, read back from its labels.
func (i *Inventory) List(ctx context.Context) ([]worker.Info, error) {
	filters, _ := json.Marshal(map[string][]string{"label": {LabelJobID}})
	path := "/containers/json?all=true&filters=" + url.QueryEscape(string(filters))
	var out []struct {
		ID      string            `json:"Id"`
		Created int64             `json:"Created"`
		Labels  map[string]string `json:"Labels"`
	}
	if err := i.eng.call(ctx, "list", http.MethodGet, path, nil, &out, http.StatusOK); err != nil {
		return nil, err
	}
	workers := make([]worker.Info, 0, len(out))
	for _, c := range out {
		workers = append(workers, worker.Info{
			ID:             c.ID,
			Created:        time.Unix(c.Created, 0),
			JobID:          parseLabelInt(c.Labels[LabelJobID]),
			RunnerName:     c.Labels[LabelRunnerName],
			Owner:          c.Labels[LabelOwner],
			Repo:           c.Labels[LabelRepo],
			InstallationID: parseLabelInt(c.Labels[LabelInstallationID]),
			RunnerID:       parseLabelInt(c.Labels[LabelRunnerID]),
		})
	}
	return workers, nil
}

// Remove force-removes container id, killing it if it runs. A container already gone is not an error.
func (i *Inventory) Remove(ctx context.Context, id string) error {
	return i.eng.removeContainer(ctx, id)
}

// parseLabelInt parses a numeric label, returning zero when it is missing or malformed.
func parseLabelInt(v string) int64 {
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}
