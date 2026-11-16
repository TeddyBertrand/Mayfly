package notes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedNow is the clock every test store uses, so timestamps are predictable.
var fixedNow = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

// newTestServer returns a test server backed by a fresh store.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(Router(NewStore(func() time.Time { return fixedNow })))
	t.Cleanup(srv.Close)
	return srv
}

// closeBody closes the response body when the test ends and fails the test if closing errors.
func closeBody(t *testing.T, resp *http.Response) {
	t.Helper()
	t.Cleanup(func() { assert.NoError(t, resp.Body.Close()) })
}

// TestCreate checks the status code POST /notes returns for each body.
func TestCreate(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"valid note", `{"text":"hello"}`, http.StatusCreated},
		{"empty text", `{"text":""}`, http.StatusBadRequest},
		{"not JSON", `hello`, http.StatusBadRequest},
	}
	srv := newTestServer(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Post(srv.URL+"/notes", "application/json", strings.NewReader(tt.body))
			require.NoError(t, err)
			closeBody(t, resp)
			assert.Equal(t, tt.want, resp.StatusCode)
		})
	}
}

// TestCreateThenGet checks that a created note can be read back by its ID.
func TestCreateThenGet(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Post(srv.URL+"/notes", "application/json", strings.NewReader(`{"text":"buy milk"}`))
	require.NoError(t, err)
	closeBody(t, resp)
	var created Note
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	require.NotEmpty(t, created.ID)

	got, err := http.Get(srv.URL + "/notes/" + created.ID)
	require.NoError(t, err)
	closeBody(t, got)
	require.Equal(t, http.StatusOK, got.StatusCode)
	var read Note
	require.NoError(t, json.NewDecoder(got.Body).Decode(&read))
	assert.Equal(t, created, read)
	assert.Equal(t, fixedNow, read.CreatedAt)
}

// TestGetUnknown checks that an unknown ID answers 404.
func TestGetUnknown(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/notes/does-not-exist")
	require.NoError(t, err)
	closeBody(t, resp)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestConcurrentAdd checks that parallel writes all land, which the RWMutex guarantees.
func TestConcurrentAdd(t *testing.T) {
	const writers = 50
	s := NewStore(time.Now)
	done := make(chan string, writers)
	for range writers {
		go func() {
			n, err := s.Add("x")
			assert.NoError(t, err)
			done <- n.ID
		}()
	}
	for range writers {
		_, ok := s.Get(<-done)
		assert.True(t, ok)
	}
}
