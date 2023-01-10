package main

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog"

	_ "github.com/lib/pq"
)

func migrateDatabase(logger zerolog.Logger, s *Settings, command, schemaName string) {
	// setup database
	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s "+
		"password=%s dbname=%s sslmode=disable",
		s.PostgresHOST, s.PostgresPort, s.PostgresUser, s.PostgresPassword, s.PostgresDB)

	pg, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		logger.Fatal().Msgf("failed to establish db connection: %v\n", err)
	}

	if err = pg.Ping(); err != nil {
		logger.Fatal().Msgf("failed to ping db: %v\n", err)
	}

	// set default
	if command == "" {
		command = "up"
	}
	// must create schema so that can set migrations table to that schema
	_, err = pg.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", schemaName))
	if err != nil {
		logger.Fatal().Err(err).Msgf("could not create schema, %s", schemaName)
	}
	goose.SetTableName(fmt.Sprintf("%s.migrations", schemaName))
	if err := goose.Run(command, pg, "migrations"); err != nil {
		logger.Fatal().Msgf("failed to apply go code migrations: %v\n", err)
	}
	// if we add any code migrations import _ "github.com/DIMO-Network/users-api/migrations" // migrations won't work without this
}
