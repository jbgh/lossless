package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"lossless/internal/inspect"
	"lossless/internal/retrieve"
	"lossless/internal/write"
)

func runInspect(args []string) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	home := homeFlag(fs)
	project := fs.String("project", "", "owner/repo (omit for all projects)")
	asJSON := fs.Bool("json", false, "print JSON")
	doAsk := fs.Bool("ask", false, "run a live ask and print why each hit packed or dropped")
	goal := fs.String("goal", "", "ask goal (with --ask)")
	question := fs.String("question", "", "ask question (with --ask)")
	session := fs.String("session", "", "session id")
	ws := fs.String("workspace", "", "workspace root (derives project if set)")
	jsonl := fs.String("jsonl", "", "session JSONL to show extract keep/skip")
	prune := fs.Bool("prune", false, "drop hook-test ingest and supersede extract-noise (this --project if set)")
	var paths stringsFlag
	fs.Var(&paths, "path", "repo-relative path for --ask (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if write.HomeIsRemote() {
		fmt.Fprintln(os.Stderr, "inspect reads the local store; unset LOSSLESS_URL or copy raw/ first")
		return 1
	}
	st, err := openStore(*home)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	var pruned *inspect.PruneResult
	if *prune {
		res, err := inspect.Prune(st, *project)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		pruned = &res
	}
	rep, err := inspect.Build(st, *project)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *doAsk {
		req := retrieve.Request{
			Project: *project, WorkspaceRoot: *ws, Goal: *goal,
			Question: *question, Paths: paths, SessionID: *session,
		}
		view, err := inspect.Ask(st, req, time.Time{})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		rep.Ask = view
	}
	if *jsonl != "" {
		ex, err := inspect.ExtractFile(*jsonl, *project)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		rep.Extract = ex
	}
	rep.Prune = pruned
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return encErr(enc.Encode(rep))
	}
	inspect.Format(os.Stdout, rep)
	return 0
}

func encErr(err error) int {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
