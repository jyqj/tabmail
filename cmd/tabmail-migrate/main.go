package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"tabmail/internal/migrate"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	dsn := os.Getenv("TABMAIL_DB_DSN")
	sub := flag.NewFlagSet(cmd, flag.ExitOnError)
	dsnFlag := sub.String("dsn", dsn, "PostgreSQL DSN")
	dirFlag := sub.String("dir", "migrations", "migration directory")
	stepsFlag := sub.Int("steps", 1, "number of migrations to revert for down")
	toFlag := sub.Int("to", 0, "target version for up")
	sub.Parse(os.Args[2:])
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	m, err := migrate.New(ctx, *dsnFlag, *dirFlag)
	if err != nil {
		fatal(err)
	}
	defer m.Close()
	switch cmd {
	case "status":
		rows, err := m.Status(ctx)
		if err != nil {
			fatal(err)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "VERSION\tAPPLIED\tDOWN\tNAME\tAPPLIED_AT")
		for _, row := range rows {
			appliedAt := "-"
			if row.AppliedAt != nil {
				appliedAt = row.AppliedAt.UTC().Format(time.RFC3339)
			}
			fmt.Fprintf(w, "%03d\t%t\t%t\t%s\t%s\n", row.Version, row.Applied, row.HasDown, row.Name, appliedAt)
		}
		_ = w.Flush()
	case "up":
		versions, err := m.Up(ctx, *toFlag)
		if err != nil {
			fatal(err)
		}
		if len(versions) == 0 {
			fmt.Println("No pending migrations.")
			return
		}
		for _, version := range versions {
			fmt.Printf("Applied %03d\n", version)
		}
	case "down":
		versions, err := m.Down(ctx, *stepsFlag)
		if err != nil {
			fatal(err)
		}
		if len(versions) == 0 {
			fmt.Println("No migrations reverted.")
			return
		}
		for _, version := range versions {
			fmt.Printf("Reverted %03d\n", version)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println("Usage: tabmail-migrate <status|up|down> [flags]")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
