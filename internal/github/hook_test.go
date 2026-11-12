package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// fakeHookAPI serves GET and PATCH /app/hook/config, checking the App JWT and recording PATCH bodies.
func fakeHookAPI(t *testing.T, patchStatus int) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var patches []map[string]any
	url := "https://old.example.com/webhook"
	mux := http.NewServeMux()
	auth := func(r *http.Request) {
		jwt, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			t.Errorf("%s without bearer token", r.Method)
			return
		}
		verifyJWT(t, jwt, &testKey(t).PublicKey)
	}
	mux.HandleFunc("GET /app/hook/config", func(w http.ResponseWriter, r *http.Request) {
		auth(r)
		_ = json.NewEncoder(w).Encode(map[string]any{"url": url, "content_type": "json", "secret": "********", "insecure_ssl": "0"})
	})
	mux.HandleFunc("PATCH /app/hook/config", func(w http.ResponseWriter, r *http.Request) {
		auth(r)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		patches = append(patches, body)
		if patchStatus != http.StatusOK {
			w.WriteHeader(patchStatus)
			_, _ = w.Write([]byte(`{"message":"Validation Failed"}`))
			return
		}
		url, _ = body["url"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{"url": url})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &patches
}

// TestSetAppWebhookURL checks the old URL is read, and only the new URL is sent with the App JWT.
func TestSetAppWebhookURL(t *testing.T) {
	srv, patches := fakeHookAPI(t, http.StatusOK)
	c := NewClient(srv.URL, 1, testKey(t))
	ctx := context.Background()

	old, err := c.AppWebhookURL(ctx)
	if err != nil || old != "https://old.example.com/webhook" {
		t.Fatalf("old url = %q, %v", old, err)
	}
	if err := c.SetAppWebhookURL(ctx, "https://new.trycloudflare.com/webhook"); err != nil {
		t.Fatal(err)
	}
	if want := []map[string]any{{"url": "https://new.trycloudflare.com/webhook"}}; !reflect.DeepEqual(*patches, want) {
		t.Errorf("patch bodies = %v, want only the url", *patches)
	}
	if now, _ := c.AppWebhookURL(ctx); now != "https://new.trycloudflare.com/webhook" {
		t.Errorf("url after set = %q", now)
	}
}

// TestSetAppWebhookURLErrors checks invalid URLs never reach the API and API failures become APIError.
func TestSetAppWebhookURLErrors(t *testing.T) {
	for _, bad := range []string{"http://insecure.example.com/webhook", "not a url", "https://", "/webhook"} {
		t.Run(bad, func(t *testing.T) {
			srv, patches := fakeHookAPI(t, http.StatusOK)
			err := NewClient(srv.URL, 1, testKey(t)).SetAppWebhookURL(context.Background(), bad)
			if err == nil || len(*patches) != 0 {
				t.Errorf("err = %v, patches = %d; want rejection before any request", err, len(*patches))
			}
		})
	}

	t.Run("api error", func(t *testing.T) {
		srv, _ := fakeHookAPI(t, http.StatusUnprocessableEntity)
		err := NewClient(srv.URL, 1, testKey(t)).SetAppWebhookURL(context.Background(), "https://ok.example.com/webhook")
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnprocessableEntity {
			t.Errorf("err = %v, want APIError 422", err)
		}
	})
}
