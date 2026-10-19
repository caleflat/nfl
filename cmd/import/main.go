package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caleflat/nfl/internal/database"
	"github.com/caleflat/nfl/internal/importer"
)

func main() {
	file := flag.String("file", "", "local uncompressed nflverse CSV")
	season := flag.Int("season", 0, "expected football season")
	source := flag.String("source", "", "source URL or provenance description")
	obtained := flag.String("source-obtained-at", "", "RFC3339 acquisition time (defaults to local file modification time)")
	timeout := flag.Duration("timeout", 5*time.Minute, "maximum import duration, including lock wait")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "import: -timeout must be positive")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	if err := run(ctx, *file, *season, *source, *obtained); err != nil {
		fmt.Fprintln(os.Stderr, "import failed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, path string, season int, source, obtained string) error {
	if path == "" || season < 1999 || season > 2100 || source == "" {
		return errors.New("-file, -season (1999–2100), and -source are required")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	acquiredAt := stat.ModTime()
	if obtained != "" {
		acquiredAt, err = time.Parse(time.RFC3339, obtained)
		if err != nil {
			return errors.New("-source-obtained-at must be RFC3339")
		}
	} else {
		fmt.Fprintln(os.Stderr, "source acquisition time defaults to file modification time; use -source-obtained-at for an exact timestamp")
	}
	db, err := database.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	start := time.Now()
	result, err := importer.Import(ctx, db, f, importer.Options{Season: season, Source: source, SourceObtained: acquiredAt})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "season %d published in %s\n", season, time.Since(start).Round(time.Millisecond))
	return json.NewEncoder(os.Stdout).Encode(result)
}
