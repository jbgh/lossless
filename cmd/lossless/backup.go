// cmd/lossless/backup.go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lossless/internal/backup"
)

// exitFor prints "lossless <prefix>: <err>" to stderr and returns the exit
// code for it: 2 for a ConfigError (backup.env missing or malformed, caught
// before any network call), else 1.
func exitFor(prefix string, err error) int {
	fmt.Fprintln(os.Stderr, "lossless "+prefix+":", err)
	var ce *backup.ConfigError
	if errors.As(err, &ce) {
		return 2
	}
	return 1
}

func runBackup(args []string) int {
	if len(args) > 0 && args[0] == "init" {
		return runBackupInit(args[1:])
	}
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	home := homeFlag(fs)
	dry := fs.Bool("dry-run", false, "plan only; write nothing to the bucket")
	verbose := fs.Bool("verbose", false, "print each uploaded file")
	takeOver := fs.Bool("take-over", false, "adopt the install that last wrote the bucket")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	sum, err := backup.Run(context.Background(), *home, backup.RunOptions{DryRun: *dry, Verbose: *verbose, TakeOver: *takeOver, Out: os.Stdout})
	if err != nil {
		return exitFor("backup", err)
	}
	switch {
	case sum.DryRun:
		fmt.Printf("dry run: scanned %d files\n", sum.Scanned)
	case sum.NoChange:
		fmt.Printf("backup: no change (scanned %d files)", sum.Scanned)
		if len(sum.Dropped) > 0 {
			fmt.Printf(", dropped %s", strings.Join(sum.Dropped, " "))
		}
		fmt.Printf(" in %s\n", sum.Elapsed.Round(time.Millisecond))
	default:
		fmt.Printf("backup: generation %s: scanned %d, uploaded %d (%d bytes), deleted %d", sum.Generation, sum.Scanned, sum.Uploaded, sum.Bytes, sum.Deleted)
		if len(sum.Dropped) > 0 {
			fmt.Printf(", dropped %s", strings.Join(sum.Dropped, " "))
		}
		fmt.Printf(", in %s\n", sum.Elapsed.Round(time.Millisecond))
	}
	return 0
}

func runBackupInit(args []string) int {
	fs := flag.NewFlagSet("backup init", flag.ContinueOnError)
	home := homeFlag(fs)
	endpoint := fs.String("endpoint", "", "S3-compatible endpoint URL (R2, B2, MinIO); empty means AWS")
	region := fs.String("region", "", "signing region (default us-east-1; an R2 endpoint signs auto by itself)")
	every := fs.Duration("every", time.Hour, "schedule interval inside serve --watch; 0 disables")
	keep := fs.Int("keep", 5, "generations to keep in the bucket")
	var target string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		if target == "" {
			target = fs.Arg(0)
		} else {
			fmt.Fprintln(os.Stderr, "lossless backup init: unexpected argument", fs.Arg(0))
			return 2
		}
		rest = fs.Args()[1:]
	}
	if target == "" {
		fmt.Fprintln(os.Stderr, "usage: lossless backup init s3://<bucket>/<prefix> [--endpoint URL] [--region R] [--every 1h] [--keep 5]")
		return 2
	}
	if err := backup.Init(*home, backup.InitOptions{URL: target, Endpoint: *endpoint, Region: *region, Every: *every, Keep: *keep}); err != nil {
		fmt.Fprintln(os.Stderr, "lossless backup init:", err)
		return 1
	}
	fmt.Printf("wrote %s and %s\n", filepath.Join(*home, "backup.env"), filepath.Join(*home, "backup.key"))
	fmt.Println("Copy backup.key somewhere that is not this machine. Restore is impossible without it.")
	if os.Getenv("LOSSLESS_BACKUP_ACCESS_KEY") == "" && os.Getenv("AWS_ACCESS_KEY_ID") == "" {
		fmt.Println("Add LOSSLESS_BACKUP_ACCESS_KEY and LOSSLESS_BACKUP_SECRET_KEY to backup.env before the first run.")
	}
	if *every > 0 {
		fmt.Printf("Schedule: every %s while lossless serve runs (picked up within a minute). Run `lossless backup` now for the first copy.\n", backup.FormatDuration(*every))
	} else {
		fmt.Println("Schedule: off. Run `lossless backup` yourself.")
	}
	return 0
}

func runRestore(args []string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	home := homeFlag(fs)
	force := fs.Bool("force", false, "union into a non-empty store; index snapshots are replaced, differing raw/export files are left alone")
	at := fs.String("at", "", "generation id from --list (default: latest)")
	list := fs.Bool("list", false, "print kept generations and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx := context.Background()
	if *list {
		gens, err := backup.List(ctx, *home)
		if err != nil {
			fmt.Fprintln(os.Stderr, "lossless restore:", err)
			return 1
		}
		fmt.Printf("%-22s %-20s %-8s %6s %12s\n", "GENERATION", "CREATED", "VERSION", "FILES", "BYTES")
		for _, g := range gens {
			mark := ""
			if g.Latest {
				mark = "  (latest)"
			}
			fmt.Printf("%-22s %-20s %-8s %6d %12d%s\n", g.Generation, g.CreatedAt, g.Lossless, g.Files, g.Bytes, mark)
		}
		return 0
	}
	sum, err := backup.Restore(ctx, *home, backup.RestoreOptions{Force: *force, At: *at, Out: os.Stderr})
	if err != nil {
		return exitFor("restore", err)
	}
	fmt.Printf("restore: generation %s: restored %d (%d bytes), skipped %d, left alone %d\n", sum.Generation, sum.Restored, sum.Bytes, sum.Skipped, sum.LeftAlone)
	for _, rel := range sum.Left {
		fmt.Printf("left alone (differs locally): %s\n", rel)
	}
	fmt.Println("Start lossless serve (or lossless setup on a new machine) to resume.")
	return 0
}
