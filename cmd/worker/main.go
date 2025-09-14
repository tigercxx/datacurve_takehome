package main

import (
	"context"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"

	"datacurve-takehome/internal/db"
	"datacurve-takehome/internal/storage"
	"datacurve-takehome/internal/worker"
)

func main() {
	// Start services
	db := db.MustOpen()
	s3c, err := storage.New(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	if err := worker.Run(os.Getenv("REDIS_ADDR"), db, s3c); err != nil {
		log.Fatal(err)
	}
}
