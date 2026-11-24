package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"lossless/internal/inspect"
	"lossless/internal/store"
	"lossless/internal/write"
)

// runMergeProject moves one project key's records under another.
//
//	lossless merge-project                       # list keys that look like another repo's
//	lossless merge-project FROM INTO [--dry-run]
//
// One checkout can be split across keys (a path-hash key from before it
// had a remote, the old name of a renamed remote), and ask reads only the
// current one. Like supersede it edits the local store offline. A merge
// cannot be undone and the list is a guess from where records were
// written, so every merge names its two keys: there is no merge-all.
func runMergeProject(args []string) int {
	fs := flag.NewFlagSet("merge-project", flag.ContinueOnError)
	home := homeFlag(fs)
	dry := fs.Bool("dry-run", false, "report what would move; change nothing")
	// Flags are read wherever they stand, so "FROM INTO --dry-run" is a
	// dry run and not a usage error or a move into a key named --dry-run.
	var rest []string
	for {
		if err := fs.Parse(args); err != nil {
			return 2
		}
		if args = fs.Args(); len(args) == 0 {
			break
		}
		rest = append(rest, args[0])
		args = args[1:]
	}
	if write.HomeIsRemote() {
		fmt.Fprintln(os.Stderr, "merge-project edits the local store; unset LOSSLESS_URL or run it on the machine holding ~/.lossless")
		return 1
	}
	bad := len(rest) != 0 && len(rest) != 2
	for _, k := range rest {
		bad = bad || strings.HasPrefix(k, "-")
	}
	if bad {
		fmt.Fprintln(os.Stderr, "usage: lossless merge-project [FROM INTO] [--dry-run]")
		return 2
	}
	st, err := openStore(*home)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()

	if len(rest) == 0 {
		list, err := inspect.SuggestMerges(st, inspect.DefaultResolve)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if len(list) == 0 {
			fmt.Println("no split projects found")
			return 0
		}
		fmt.Println("keys whose records were written from another repo's checkout:")
		for _, s := range list {
			fmt.Printf("  %-28s → %-24s %4d active  (%d of %d records name a folder; %.0f%% of those are now %s)\n",
				s.From, s.Into, s.Active, s.Located, s.Total, s.Share*100, s.Into)
		}
		fmt.Println("check each one, then merge it with: lossless merge-project FROM INTO (add --dry-run to preview)")
		return 0
	}
	code := mergeOne(st, rest[0], rest[1], *dry)
	if code == 0 && !*dry {
		fmt.Println("restart the daemon so its caches read the merged project")
	}
	return code
}

func mergeOne(st *store.Store, from, into string, dry bool) int {
	res, err := st.MoveProject(from, into, dry)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if res.Moved == 0 && res.Sessions == 0 {
		fmt.Fprintf(os.Stderr, "nothing is stored under %s; lossless merge-project lists the keys that look split\n", res.From)
		return 1
	}
	verb := "moved"
	if dry {
		verb = "would move"
	}
	fmt.Printf("%s %d records from %s to %s (%d active, %d retired as duplicates), %d sessions\n",
		verb, res.Moved, res.From, res.Into, res.Active, res.Deduped, res.Sessions)
	return 0
}

// splitDetail is doctor's line about project keys that hold one
// repository's records under another name.
func splitDetail(list []inspect.MergeSuggestion) string {
	if len(list) == 0 {
		return "no repository is split across project keys"
	}
	parts := make([]string, 0, 3)
	for i, s := range list {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("and %d more", len(list)-3))
			break
		}
		parts = append(parts, fmt.Sprintf("%s → %s, %d records", s.From, s.Into, s.Active))
	}
	return fmt.Sprintf("%d split (%s); an ask sees only one part until you run: lossless merge-project", len(list), strings.Join(parts, "; "))
}
