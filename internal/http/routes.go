package http

import (
	"encoding/json"
	"fmt"
	"net/http"

	"database/sql"

	"github.com/go-chi/chi/v5"
	m "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jmoiron/sqlx"

	"datacurve-takehome/internal/auth"
	"datacurve-takehome/internal/db"
	"datacurve-takehome/internal/schemas"
	"datacurve-takehome/internal/storage"
)

type Server struct {
	DB    *sqlx.DB
	S3    *storage.Client
	Asynq *asynq.Client
}

func NewServer(dbx *sqlx.DB, s3c *storage.Client, asq *asynq.Client) *http.Server {
	s := &Server{DB: dbx, S3: s3c, Asynq: asq}
	r := chi.NewRouter()
	r.Use(m.RequestID, m.RealIP, m.Logger, m.Recoverer)

	// Admin/API-token protected
	r.Group(func(r chi.Router) {
		r.Use(RequireAPIToken)
		r.Post("/traces", s.createTrace)
		r.Post("/traces/{id}/finalize", s.finalize)
		r.Post("/traces/{id}/qa", s.runQA)
		r.Get("/traces/{id}", s.getTrace)
	})

	// Upload token (uses Authorization: Bearer <upload>)
	r.Post("/traces/{id}/events", s.appendEvents)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// just a simple ping endpoint
		if err := dbx.Ping(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"status":"db error"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	})

	return &http.Server{Addr: ":8000", Handler: r}
}

type createResp struct {
	TraceID     string `json:"trace_id"`
	UploadToken string `json:"upload_token"`
}

type acceptedResp struct {
	Accepted int   `json:"accepted"`
	NextSeq  int64 `json:"next_seq"`
}

type errResp struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) createTrace(w http.ResponseWriter, r *http.Request) {
	var req schemas.CreateTraceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, errResp{err.Error()})
		return
	}
	id := uuid.NewString()
	upload := uuid.NewString()
	dev, _ := json.Marshal(req.Developer)
	task, _ := json.Marshal(req.Task)
	env, _ := json.Marshal(req.Environment)

	_, err := s.DB.Exec(`insert into traces(id, developer, task, environment, upload_token_hash) values($1,$2,$3,$4,$5)`, id, dev, task, env, auth.HashToken(upload))
	if err != nil {
		writeJSON(w, 500, errResp{err.Error()})
		return
	}
	fmt.Println("Created trace:", id)
	fmt.Println("Upload token:", upload)
	writeJSON(w, 200, createResp{TraceID: id, UploadToken: upload})
}

func (s *Server) appendEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	got := r.Header.Get("Authorization")
	if len(got) < 8 || got[:7] != "Bearer " {
		writeJSON(w, 401, errResp{"missing bearer"})
		return
	}
	upload := got[7:]

	var cnt int
	if err := s.DB.Get(&cnt, `select count(1) from traces where id=$1 and status='open' and upload_token_hash=$2`, id, auth.HashToken(upload)); err != nil || cnt == 0 {
		writeJSON(w, 404, errResp{"trace not found or sealed"})
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, 400, errResp{err.Error()})
		return
	}
	ref, err := s.S3.PutJSON(r.Context(), payload)
	if err != nil {
		writeJSON(w, 500, errResp{err.Error()})
		return
	}
	var maxSeq sql.NullInt64
	_ = s.DB.Get(&maxSeq, `select coalesce(max(seq), -1) from event_batches where trace_id=$1`, id)
	next := maxSeq.Int64 + 1
	events := 0
	if evs, ok := payload["events"].([]any); ok {
		events = len(evs)
	}

	_, err = s.DB.Exec(`insert into event_batches(id, trace_id, seq, object_ref, event_count) values($1,$2,$3,$4,$5)`, uuid.NewString(), id, next, ref, events)
	if err != nil {
		writeJSON(w, 500, errResp{err.Error()})
		return
	}
	writeJSON(w, 200, acceptedResp{Accepted: events, NextSeq: next + 1})
}

func (s *Server) finalize(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.DB.Exec(`update traces set status='sealed' where id=$1`, id); err != nil {
		writeJSON(w, 500, errResp{err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "sealed"})
}

func (s *Server) runQA(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task := asynq.NewTask("run_full_qa", []byte(id))
	if _, err := s.Asynq.Enqueue(task, asynq.MaxRetry(0)); err != nil {
		writeJSON(w, 500, errResp{err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"enqueued": "ok"})
}

func (s *Server) getTrace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var t db.Trace
	if err := s.DB.Get(&t, `select * from traces where id=$1`, id); err != nil {
		writeJSON(w, 404, errResp{"not found"})
		return
	}
	var out schemas.TraceOut
	out.TraceID = t.ID
	out.CreatedAt = t.CreatedAt
	out.Version = t.Version
	_ = json.Unmarshal(t.Developer, &out.Developer)
	_ = json.Unmarshal(t.Task, &out.Task)
	_ = json.Unmarshal(t.Environment, &out.Environment)
	if len(t.Artifacts) > 0 {
		_ = json.Unmarshal(t.Artifacts, &out.Artifacts)
	}
	if len(t.QA) > 0 {
		_ = json.Unmarshal(t.QA, &out.QA)
	}
	writeJSON(w, 200, out)
}
