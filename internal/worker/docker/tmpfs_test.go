package docker

import (
	"reflect"
	"strconv"
	"testing"
)

// TestTmpfsSpecSplitsBudget checks the share split, the mount options and the tool variables.
func TestTmpfsSpecSplitsBudget(t *testing.T) {
	mounts, env := tmpfsSpec(1500 << 20)
	share := int64(1500<<20) / 6
	want := map[string]string{
		"/home/runner/_work": "size=" + itoa(2*share) + ",mode=0755,uid=1001,gid=1001",
		"/tmp":               "size=" + itoa(share) + ",mode=1777,uid=1001,gid=1001",
		"/mayfly/go-build":   "size=" + itoa(share) + ",mode=0755,uid=1001,gid=1001",
		"/mayfly/go-mod":     "size=" + itoa(share) + ",mode=0755,uid=1001,gid=1001",
		"/mayfly/npm":        "size=" + itoa(share) + ",mode=0755,uid=1001,gid=1001",
	}
	if !reflect.DeepEqual(mounts, want) {
		t.Errorf("mounts = %v, want %v", mounts, want)
	}
	wantEnv := []string{"GOCACHE=/mayfly/go-build", "GOMODCACHE=/mayfly/go-mod", "npm_config_cache=/mayfly/npm"}
	if !reflect.DeepEqual(env, wantEnv) {
		t.Errorf("env = %v, want %v", env, wantEnv)
	}
}

// TestTmpfsSpecOff checks that a zero budget adds no mounts and no variables.
func TestTmpfsSpecOff(t *testing.T) {
	if mounts, env := tmpfsSpec(0); mounts != nil || env != nil {
		t.Errorf("tmpfsSpec(0) = %v, %v, want nil, nil", mounts, env)
	}
}

// TestTmpfsNeverHidesToolCache checks that no mount covers the toolchains baked into the image.
func TestTmpfsNeverHidesToolCache(t *testing.T) {
	const toolCache = "/opt/hostedtoolcache"
	for _, m := range tmpfsMounts {
		if m.Path == "/" || m.Path == "/opt" || m.Path == toolCache {
			t.Errorf("mount %s hides %s", m.Path, toolCache)
		}
	}
}

// itoa formats n in base 10 for the expected mount options.
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
