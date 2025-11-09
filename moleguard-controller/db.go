package main

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func initDB() *sql.DB {
	db, err := sql.Open("sqlite3", "./moleguard.db")
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec("create table if not exists users(token text primary key)")
	if err != nil {
		log.Fatal(err)
	}

	return db
}
