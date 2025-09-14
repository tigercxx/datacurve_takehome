package db

import "time"

type Trace struct {
	ID              string    `db:"id"`
	CreatedAt       time.Time `db:"created_at"`
	Developer       []byte    `db:"developer"`
	Task            []byte    `db:"task"`
	Environment     []byte    `db:"environment"`
	Status          string    `db:"status"`
	UploadTokenHash string    `db:"upload_token_hash"`
	Artifacts       []byte    `db:"artifacts"`
	QA              []byte    `db:"qa"`
	Version         string    `db:"version"`
}

type EventBatch struct {
	ID         string    `db:"id"`
	TraceID    string    `db:"trace_id"`
	Seq        int64     `db:"seq"`
	ObjectRef  string    `db:"object_ref"`
	CreatedAt  time.Time `db:"created_at"`
	EventCount int64     `db:"event_count"`
}
