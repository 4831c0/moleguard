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
	_, err = db.Exec(`create table if not exists device(
		id int,
		node text,
		user_token text,
		config text,
		ip text,
		foreign key(user_token) references users(token),
		primary key (id, node)
	)`)
	if err != nil {
		log.Fatal(err)
	}

	return db
}
