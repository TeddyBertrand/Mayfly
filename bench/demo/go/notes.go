// Package notes is a small in-memory notes API, the Go workload of the Mayfly demo repo.
package notes

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// maxBodyBytes caps a request body so one client cannot exhaust memory.
const maxBodyBytes = 1 << 20

// Note is one stored note.
type Note struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// Store keeps notes in memory, safe for concurrent use.
type Store struct {
	mu    sync.RWMutex
	notes map[string]Note
	now   func() time.Time
}

// NewStore returns an empty store that stamps notes with now.
func NewStore(now func() time.Time) *Store {
	return &Store{notes: make(map[string]Note), now: now}
}

// ErrEmptyText is returned when a note has no text.
var ErrEmptyText = errors.New("note text is empty")

// Add stores a new note and returns it with its generated ID.
func (s *Store) Add(text string) (Note, error) {
	if text == "" {
		return Note{}, ErrEmptyText
	}
	n := Note{ID: uuid.NewString(), Text: text, CreatedAt: s.now().UTC()}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notes[n.ID] = n
	return n, nil
}

// Get returns the note with the given ID and whether it exists.
func (s *Store) Get(id string) (Note, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.notes[id]
	return n, ok
}

// Router returns the HTTP routes for the store: POST /notes and GET /notes/{id}.
func Router(s *Store) http.Handler {
	r := chi.NewRouter()
	r.Post("/notes", s.handleCreate)
	r.Get("/notes/{id}", s.handleGet)
	return r
}

// handleCreate decodes {"text": ...} and answers 201 with the stored note.
func (s *Store) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	n, err := s.Add(req.Text)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

// handleGet answers 200 with the note, or 404 when the ID is unknown.
func (s *Store) handleGet(w http.ResponseWriter, r *http.Request) {
	n, ok := s.Get(chi.URLParam(r, "id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

// writeJSON writes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
