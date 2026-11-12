package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestGenerateJITConfig checks the request sent to GitHub and the decoded response.
func TestGenerateJITConfig(t *testing.T) {
	f := newFakeGitHub(t, time.Now().Add(time.Hour))
	var gotBody map[string]any
	var gotAuth string
	f.mux.HandleFunc("POST /repos/octo/demo/actions/runners/generate-jitconfig", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"runner":{"id":77,"name":"x"},"encoded_jit_config":"ZW5jb2RlZA=="}`))
	})
	c, _ := newTestClient(t, f, time.Now())

	cfg, err := c.GenerateJITConfig(context.Background(), 1, "octo/demo", "mayfly-42-abcdef", []string{"self-hosted", "mayfly"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RunnerID != 77 || cfg.Encoded != "ZW5jb2RlZA==" {
		t.Errorf("got %+v", cfg)
	}
	if gotAuth != "Bearer tok-1-1" {
		t.Errorf("authorization = %q, want the installation token", gotAuth)
	}
	want := map[string]any{
		"name":            "mayfly-42-abcdef",
		"runner_group_id": float64(1),
		"labels":          []any{"self-hosted", "mayfly"},
		"work_folder":     "_work",
	}
	if !reflect.DeepEqual(gotBody, want) {
		t.Errorf("body = %v, want %v", gotBody, want)
	}
}

// TestGenerateJITConfigErrors checks invalid repos and API failures.
func TestGenerateJITConfigErrors(t *testing.T) {
	f := newFakeGitHub(t, time.Now().Add(time.Hour))
	f.mux.HandleFunc("POST /repos/octo/taken/actions/runners/generate-jitconfig", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"Already exists"}`))
	})
	c, _ := newTestClient(t, f, time.Now())

	tests := []struct {
		name       string
		repo       string
		wantStatus int
	}{
		{"path traversal", "../../app", 0},
		{"no owner", "demo", 0},
		{"extra segment", "octo/demo/x", 0},
		{"conflict", "octo/taken", http.StatusConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.GenerateJITConfig(context.Background(), 1, tt.repo, "n", nil)
			if err == nil {
				t.Fatal("expected error")
			}
			var apiErr *APIError
			isAPI := errors.As(err, &apiErr)
			if tt.wantStatus == 0 && isAPI {
				t.Errorf("invalid repo reached the API: %v", err)
			}
			if tt.wantStatus != 0 && (!isAPI || apiErr.Status != tt.wantStatus) {
				t.Errorf("err = %v, want APIError %d", err, tt.wantStatus)
			}
		})
	}
}

// TestJITConfigRedacted checks that fmt and slog never print the encoded config.
func TestJITConfigRedacted(t *testing.T) {
	cfg := JITConfig{RunnerID: 7, Encoded: "SECRET-BLOB"}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("jit", "cfg", cfg)

	for name, out := range map[string]string{
		"fmt %v":  fmt.Sprintf("%v", cfg),
		"fmt %+v": fmt.Sprintf("%+v", cfg),
		"slog":    buf.String(),
	} {
		if strings.Contains(out, "SECRET-BLOB") {
			t.Errorf("%s leaks encoded config: %s", name, out)
		}
	}
}

// TestRunnerName checks the job ID is embedded and names are unique.
func TestRunnerName(t *testing.T) {
	pattern := regexp.MustCompile(`^mayfly-123456789-[0-9a-f]{6}$`)
	seen := map[string]bool{}
	for range 100 {
		name, err := RunnerName(123456789)
		if err != nil {
			t.Fatal(err)
		}
		if !pattern.MatchString(name) {
			t.Fatalf("name %q does not match %s", name, pattern)
		}
		seen[name] = true
	}
	if len(seen) < 99 {
		t.Errorf("only %d unique names out of 100", len(seen))
	}
}
