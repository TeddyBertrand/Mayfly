package docker

import "fmt"

// runnerUID and runnerGID own the tmpfs mounts, so the runner user of the official image can write to them.
const (
	runnerUID = 1001
	runnerGID = 1001
)

// tmpfsMount is one worker directory kept in memory (decision 029). Env, when set, points a tool at Path,
// since mounting under $HOME would leave root-owned parent directories the runner cannot write to.
type tmpfsMount struct {
	Path   string
	Env    string
	Mode   string
	Shares int64
}

// tmpfsMounts are the worker directories on tmpfs. The budget is split by shares; _work holds the
// checkout, node_modules and build outputs, so it gets the largest one.
var tmpfsMounts = []tmpfsMount{
	{Path: "/home/runner/_work", Mode: "0755", Shares: 2},
	{Path: "/tmp", Mode: "1777", Shares: 1},
	{Path: "/mayfly/go-build", Env: "GOCACHE", Mode: "0755", Shares: 1},
	{Path: "/mayfly/go-mod", Env: "GOMODCACHE", Mode: "0755", Shares: 1},
	{Path: "/mayfly/npm", Env: "npm_config_cache", Mode: "0755", Shares: 1},
}

// tmpfsSpec returns the HostConfig.Tmpfs map and the environment variables for a budget of total
// bytes. A total of 0 returns nil for both.
func tmpfsSpec(total int64) (map[string]string, []string) {
	if total <= 0 {
		return nil, nil
	}
	var shares int64
	for _, m := range tmpfsMounts {
		shares += m.Shares
	}
	mounts := make(map[string]string, len(tmpfsMounts))
	var env []string
	for _, m := range tmpfsMounts {
		mounts[m.Path] = fmt.Sprintf("size=%d,mode=%s,uid=%d,gid=%d", total/shares*m.Shares, m.Mode, runnerUID, runnerGID)
		if m.Env != "" {
			env = append(env, m.Env+"="+m.Path)
		}
	}
	return mounts, env
}
