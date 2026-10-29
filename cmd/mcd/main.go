package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/Niclassslua/MoodleMCP/internal/app"
	"github.com/Niclassslua/MoodleMCP/internal/config"
	mcpserver "github.com/Niclassslua/MoodleMCP/internal/mcp"
	"github.com/Niclassslua/MoodleMCP/internal/moodle"
)

const version = "0.5.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "list":
		os.Exit(runList(args))
	case "inspect":
		os.Exit(runInspect(args))
	case "due":
		os.Exit(runDue(args))
	case "grades":
		os.Exit(runGrades(args))
	case "download":
		os.Exit(runDownload(args))
	case "sync":
		os.Exit(runSync(args))
	case "forums":
		os.Exit(runForums(args))
	case "discussion":
		os.Exit(runDiscussion(args))
	case "login":
		os.Exit(runLogin(args))
	case "logout":
		if err := config.Logout(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "messages":
		os.Exit(runMessages(args))
	case "doctor":
		os.Exit(runDoctor(args))
	case "mcp":
		os.Exit(runMCP(args))
	case "version", "-v", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `mcd — Moodle Course Downloader (Web Services CLI + MCP)

Usage:
  mcd login --url URL --username USER   (password from stdin or prompt)
  mcd logout
  mcd list [--filter all|inprogress|past|future] [--json=false]
  mcd inspect COURSE [--json]
  mcd due [--days 14] [COURSE] [--json]
  mcd grades COURSE [--json]
  mcd download COURSE [options]
  mcd sync COURSE | --file courses.json [options]
  mcd forums COURSE [query] [--json]
  mcd discussion ID [--rich] [--json]
  mcd messages [query] [--json] [--export] [--out DIR]
  mcd doctor [--json]
  mcd mcp serve
  mcd version

COURSE may be id, URL, shortname, or unique name substring.

Download/sync options:
  --out DIR              output directory (default: ./downloads)
  --mode MODE            all | resources-only | forums-only (default: all)
  --filename-mode MODE   utf8 | ascii
  --concurrency N        parallel downloads (default: 4)
  --force                re-download everything
  --skip-locked          leave out locked forum discussions
  --flat                 write into DIR itself, no course-name folder
  --max-size MB          list but do not download larger files
  --json                 print the result (incl. the course's file list) as JSON
  --interval DURATION    sync loop (e.g. 1h); 0 = once

Flags go before COURSE. Credentials: mcd login, or
Env: MOODLE_URL, MOODLE_USERNAME, MOODLE_PASSWORD
     MOODLE_TOKEN, MOODLE_SERVICE, MCD_FILENAME_MODE
`)
}

func mustApp() *app.App {
	a, err := app.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	return a
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func runList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	asJSON := fs.Bool("json", true, "print JSON")
	filter := fs.String("filter", "all", "all|inprogress|past|future")
	_ = fs.Parse(args)
	a := mustApp()
	courses, err := a.CoursesFiltered(*filter)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *asJSON {
		printJSON(map[string]any{"courses": courses})
		return 0
	}
	for _, co := range courses {
		fmt.Printf("%d\t%s\t%s\n", co.ID, co.ShortName, co.FullName)
	}
	return 0
}

func runInspect(args []string) int {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	asJSON := fs.Bool("json", true, "print JSON")
	ref, rest := peelCourse(args)
	_ = fs.Parse(rest)
	if ref == "" && fs.NArg() > 0 {
		ref = fs.Arg(0)
	}
	if ref == "" {
		fmt.Fprintln(os.Stderr, "error: COURSE required")
		return 2
	}
	a := mustApp()
	res, err := a.Inspect(ref)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *asJSON {
		printJSON(res)
		return 0
	}
	fmt.Printf("%s (%s) id=%d\n", res.Course.FullName, res.Course.ShortName, res.Course.ID)
	fmt.Printf("files: %d (%.1f MB)\n", res.FileCount, float64(res.FileBytes)/1e6)
	for k, v := range res.ModuleCounts {
		fmt.Printf("  %s: %d\n", k, v)
	}
	return 0
}

func runDue(args []string) int {
	fs := flag.NewFlagSet("due", flag.ExitOnError)
	days := fs.Int("days", 14, "lookahead days")
	asJSON := fs.Bool("json", true, "print JSON")
	_ = fs.Parse(args)
	ref := ""
	if fs.NArg() > 0 {
		ref = fs.Arg(0)
	}
	a := mustApp()
	res, err := a.Due(ref, *days, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *asJSON {
		printJSON(res)
		return 0
	}
	for _, e := range res.Events {
		when := time.Unix(e.When, 0).Local().Format("2006-01-02 15:04")
		fmt.Printf("%s\t%s\t%s\n", when, e.EventType, e.Name)
	}
	return 0
}

func runGrades(args []string) int {
	fs := flag.NewFlagSet("grades", flag.ExitOnError)
	asJSON := fs.Bool("json", true, "print JSON")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: COURSE required")
		return 2
	}
	a := mustApp()
	res, err := a.Grades(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *asJSON {
		printJSON(res)
		return 0
	}
	fmt.Printf("%s\n", res.Course.FullName)
	for _, it := range res.Items {
		if it.ItemName == "" {
			continue
		}
		fmt.Printf("%s\t%s\t/%g\n", it.ItemName, it.GradeFormatted, it.GradeMax)
	}
	return 0
}

func dlFlags(fs *flag.FlagSet) (out, mode, fname *string, conc *int, force *bool) {
	defaultName := os.Getenv("MCD_FILENAME_MODE")
	if defaultName == "" {
		defaultName = "utf8"
	}
	out = fs.String("out", "./downloads", "output directory")
	mode = fs.String("mode", "all", "all|resources-only|forums-only")
	fname = fs.String("filename-mode", defaultName, "utf8|ascii")
	conc = fs.Int("concurrency", 4, "parallel downloads")
	force = fs.Bool("force", false, "re-download everything")
	return
}

type syncFlags struct {
	skipLocked, flat, asJSON *bool
	maxMB                    *int
}

func addSyncFlags(fs *flag.FlagSet) syncFlags {
	return syncFlags{
		skipLocked: fs.Bool("skip-locked", false, "leave out forum discussions marked Gesperrt (locked)"),
		flat:       fs.Bool("flat", false, "write into --out itself, no course-name folder"),
		asJSON:     fs.Bool("json", false, "print the result as JSON on stdout"),
		maxMB:      fs.Int("max-size", 0, "skip files larger than this many MB (0 = no limit)"),
	}
}

func peelCourse(args []string) (ref string, rest []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

func runDownload(args []string) int {
	fs := flag.NewFlagSet("download", flag.ExitOnError)
	id := fs.Int("id", 0, "course id (legacy)")
	courseURL := fs.String("url", "", "course URL (legacy)")
	out, mode, fname, conc, force := dlFlags(fs)
	sf := addSyncFlags(fs)
	ref, rest := peelCourse(args)
	_ = fs.Parse(rest)
	if ref == "" && fs.NArg() > 0 {
		ref = fs.Arg(0)
	}
	if ref == "" && *id > 0 {
		ref = strconv.Itoa(*id)
	}
	if ref == "" && *courseURL != "" {
		ref = *courseURL
	}
	if ref == "" {
		fmt.Fprintln(os.Stderr, "error: COURSE required")
		return 2
	}

	a := mustApp()
	fmt.Fprintf(os.Stderr, "Downloading %s …\n", ref)
	res, err := a.Download(app.DownloadOpts{
		Course: ref, OutDir: *out, Mode: *mode, FilenameMode: *fname,
		Concurrency: *conc, Force: *force, SkipLocked: *sf.skipLocked,
		Flat: *sf.flat, MaxBytes: int64(*sf.maxMB) << 20,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *sf.asJSON {
		printJSON(res)
	}
	fmt.Fprintf(os.Stderr, "Done: %q — %d files, %d skipped → %s\n",
		res.CourseName, res.Downloaded, res.Skipped, res.CourseDir)
	return 0
}

func runSync(args []string) int {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	file := fs.String("file", "", "JSON file with course list")
	interval := fs.String("interval", "0", "repeat interval")
	out, mode, fname, conc, force := dlFlags(fs)
	sf := addSyncFlags(fs)
	ref, rest := peelCourse(args)
	_ = fs.Parse(rest)
	if ref == "" && fs.NArg() > 0 {
		ref = fs.Arg(0)
	}

	a := mustApp()
	var items []app.SyncItem

	if *file != "" {
		data, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		var raw []struct {
			CourseID int    `json:"courseId"`
			Course   string `json:"course"`
			URL      string `json:"courseUrl"`
			Out      string `json:"out"`
			Mode     string `json:"mode"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		for _, it := range raw {
			ref := it.Course
			if ref == "" && it.CourseID > 0 {
				ref = strconv.Itoa(it.CourseID)
			}
			if ref == "" {
				ref = it.URL
			}
			items = append(items, app.SyncItem{Course: ref, Out: it.Out, Mode: it.Mode})
		}
	} else if ref != "" {
		items = append(items, app.SyncItem{Course: ref, Out: *out, Mode: *mode})
	} else {
		fmt.Fprintln(os.Stderr, "error: COURSE or --file required")
		return 2
	}

	var every time.Duration
	if *interval != "0" && *interval != "" {
		var err error
		every, err = time.ParseDuration(*interval)
		if err != nil {
			if ms, e2 := strconv.ParseInt(*interval, 10, 64); e2 == nil {
				every = time.Duration(ms) * time.Millisecond
			} else {
				fmt.Fprintln(os.Stderr, "error: bad --interval:", err)
				return 2
			}
		}
	}

	for {
		results, err := a.SyncOnce(app.SyncOpts{
			Items: items, OutDir: *out, Mode: *mode, FilenameMode: *fname,
			Concurrency: *conc, Force: *force, SkipLocked: *sf.skipLocked,
			Flat: *sf.flat, MaxBytes: int64(*sf.maxMB) << 20,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if *sf.asJSON {
			printJSON(map[string]any{"courses": results})
		}
		for _, res := range results {
			fmt.Fprintf(os.Stderr, "  %q — %d new, %d skipped → %s\n",
				res.CourseName, res.Downloaded, res.Skipped, res.CourseDir)
		}
		if every <= 0 {
			return 0
		}
		fmt.Fprintf(os.Stderr, "Sleeping %s …\n", every)
		time.Sleep(every)
	}
}

func runForums(args []string) int {
	fs := flag.NewFlagSet("forums", flag.ExitOnError)
	asJSON := fs.Bool("json", true, "print JSON")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: COURSE required")
		return 2
	}
	query := ""
	if fs.NArg() > 1 {
		query = strings.Join(fs.Args()[1:], " ")
	}
	a := mustApp()
	res, err := a.SearchForums(fs.Arg(0), query)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *asJSON {
		printJSON(res)
		return 0
	}
	for _, h := range res.Hits {
		fmt.Printf("[%s] %s (%d posts)\n", h.Forum, h.Discussion, h.Posts)
	}
	return 0
}

func runDiscussion(args []string) int {
	fs := flag.NewFlagSet("discussion", flag.ExitOnError)
	asJSON := fs.Bool("json", true, "print JSON")
	rich := fs.Bool("rich", false, "whole thread with HTML, reply structure and images as data URIs")
	_ = fs.Parse(args)
	id, err := strconv.Atoi(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: discussion ID required")
		return 2
	}
	a := mustApp()
	res, err := a.DiscussionPosts(id, 0, 100)
	if *rich {
		res, err = a.DiscussionThread(id)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *asJSON {
		printJSON(res)
		return 0
	}
	for _, p := range res.Posts {
		fmt.Printf("[%s] %s: %s\n%s\n\n", p.Time, p.Author, p.Subject, p.Text)
	}
	return 0
}

// runLogin fetches a token with the password once and keeps only the token (OS keychain).
func runLogin(args []string) int {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	url := fs.String("url", "", "Moodle base URL, e.g. https://moodle.example.edu")
	user := fs.String("username", "", "Moodle username")
	_ = fs.Parse(args)
	base := strings.TrimRight(strings.TrimSpace(*url), "/")
	if base == "" || *user == "" {
		fmt.Fprintln(os.Stderr, "error: --url and --username required")
		return 2
	}
	var pass string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		pass = string(b)
	} else {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		pass = strings.TrimRight(line, "\r\n")
	}
	if pass == "" {
		fmt.Fprintln(os.Stderr, "error: empty password")
		return 2
	}
	c, err := moodle.New(config.Config{BaseURL: base, Username: *user, Password: pass, Service: envOr("MOODLE_SERVICE", "moodle_mobile_app")})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if err := config.SaveLogin(base, *user, c.Token); err != nil {
		fmt.Fprintln(os.Stderr, "error: keychain:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "Logged in to %s as %s\n", base, *user)
	return 0
}

func runMessages(args []string) int {
	fs := flag.NewFlagSet("messages", flag.ExitOnError)
	asJSON := fs.Bool("json", true, "print JSON")
	export := fs.Bool("export", false, "write conversations as markdown")
	out := fs.String("out", "./downloads", "output directory for --export")
	fname := fs.String("filename-mode", envOr("MCD_FILENAME_MODE", "utf8"), "utf8|ascii")
	_ = fs.Parse(args)
	query := strings.Join(fs.Args(), " ")
	a := mustApp()
	if *export {
		res, err := a.ExportMessages(*out, query, *fname)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if *asJSON {
			printJSON(res)
			return 0
		}
		fmt.Fprintf(os.Stderr, "Exported %d conversations → %s\n", res.Written, res.Dir)
		return 0
	}
	res, err := a.ListMessages(query)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *asJSON {
		printJSON(res)
		return 0
	}
	for _, h := range res.Hits {
		fmt.Printf("%d\t%s\t%d msgs\tunread=%d\t%s\n", h.ID, h.Title, h.Count, h.Unread, h.Members)
		for _, m := range h.Messages {
			fmt.Printf("  [%s] %s: %s\n", m.Time, m.From, m.Text)
		}
	}
	return 0
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func runDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	asJSON := fs.Bool("json", true, "print JSON")
	_ = fs.Parse(args)
	a := mustApp()
	res := a.Doctor()
	if *asJSON {
		printJSON(res)
	} else {
		fmt.Printf("ok=%v site=%s user=%s release=%s courses=%d downloadfiles=%d\n",
			res.OK, res.SiteName, res.Username, res.Release, res.CourseCount, res.DownloadFiles)
		if res.Error != "" {
			fmt.Println("error:", res.Error)
		}
	}
	if !res.OK {
		return 1
	}
	return 0
}

func runMCP(args []string) int {
	if len(args) < 1 || args[0] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: mcd mcp serve")
		return 2
	}
	if err := mcpserver.Serve(version); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}
