// Command migrate applies database migrations.
//
//	go run ./cmd/migrate status
//	go run ./cmd/migrate up
//	go run ./cmd/migrate baseline <version>   # adopt a DB migrated by hand / built from schema.sql
//
// Uses MIGRATE_DATABASE_URL (the greenroot_migrator role) when set, otherwise
// DATABASE_URL. In production the API's DATABASE_URL role (greenroot_app)
// cannot run DDL, so MIGRATE_DATABASE_URL is required there.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database/migrate"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database/migrations"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: migrate status | up | baseline <version>")
	}
	cfg := config.Load()
	if u := os.Getenv("MIGRATE_DATABASE_URL"); u != "" {
		cfg.Database.URL = u
	}
	if cfg.Database.URL == "" {
		return fmt.Errorf("MIGRATE_DATABASE_URL or DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	db, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()
	all, err := migrate.Load(migrations.FS)
	if err != nil {
		return err
	}
	runner := migrate.New(db, all)

	switch args[0] {
	case "status":
		lines, err := runner.Status(ctx)
		if err != nil {
			return err
		}
		for _, l := range lines {
			state := "pending"
			switch {
			case l.Baselined:
				state = "baselined " + l.AppliedAt.Format(time.RFC3339)
			case l.Applied:
				state = "applied   " + l.AppliedAt.Format(time.RFC3339)
			}
			fmt.Printf("%-55s %s\n", l.Version, state)
		}
	case "up":
		done, err := runner.Up(ctx)
		for _, v := range done {
			fmt.Println("applied", v)
		}
		if err != nil {
			return err
		}
		if len(done) == 0 {
			fmt.Println("up to date")
		}
	case "baseline":
		if len(args) != 2 {
			return fmt.Errorf("usage: migrate baseline <version>")
		}
		marked, err := runner.Baseline(ctx, args[1])
		for _, v := range marked {
			fmt.Println("baselined", v)
		}
		return err
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	return nil
}
