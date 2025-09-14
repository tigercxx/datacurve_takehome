package main

import (
	"context"
	"log"
	"os"

	"github.com/hibiken/asynq"

	"datacurve-takehome/internal/db"
	httpSrv "datacurve-takehome/internal/http"
	"datacurve-takehome/internal/migrations"
	"datacurve-takehome/internal/storage"
)

func main() {
	// Run embedded migrations (idempotent)
	migrations.Run()

	// Start services
	dbase := db.MustOpen()
	s3c, err := storage.New(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	asq := asynq.NewClient(asynq.RedisClientOpt{Addr: os.Getenv("REDIS_ADDR")})
	srv := httpSrv.NewServer(dbase, s3c, asq)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
