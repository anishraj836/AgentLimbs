package db

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func InitDB(databaseURL string) {
	if databaseURL == "" {
		databaseURL = "postgres://crawler:crawler_password@localhost:5432/crawler_db"
	}
	var err error
	Pool, err = pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		panic("Unable to connect to database: " + err.Error())
	}
}

func CloseDB() {
	if Pool != nil {
		Pool.Close()
	}
}
