// Command before-yes serves the Before Yes API and app, runs one check from
// the terminal, or prints the settings plan.
//
//	before-yes serve  [-config before-yes.toml]
//	before-yes check  [-config before-yes.toml] [-age 9] Roblox
//	before-yes config [-config before-yes.toml]
//	before-yes seed-cards [-config before-yes.toml] [-dir testdata/demo-cards]
//	before-yes backup     [-config before-yes.toml] [-keep 14]
//	before-yes usage      [-config before-yes.toml] [-days 14]
package main

import (
	"context"
	"errors"
	goflag "flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// run is main without the process: it returns the exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	log := slog.New(slog.NewTextHandler(stderr, nil))
	if len(args) < 1 {
		usage(stderr)
		return 2
	}
	cmd := args[0]
	o, ok := parseArgs(cmd, args[1:], stderr)
	if !ok {
		return 2
	}
	cfgPath, age := &o.config, &o.age
	s, plan, err := Load(ctx, *cfgPath, nil)
	if err != nil {
		log.Error("before-yes: settings", "err", err)
		return 1
	}
	switch cmd {
	case "serve":
		err = serve(ctx, log, s)
	case "check":
		err = check(ctx, log, s, o.title, *age, stdout, stderr)
	case "config":
		_, err = fmt.Fprint(stdout, plan.String())
	case "seed-cards":
		if o.title != "" {
			usage(stderr)
			return 2
		}
		err = seedCards(ctx, s, o.dir, stdout)
	case "backup":
		if o.title != "" {
			usage(stderr)
			return 2
		}
		err = backupData(ctx, s, o.keep, stdout)
	case "usage":
		if o.title != "" {
			usage(stderr)
			return 2
		}
		err = usageReport(ctx, s, o.days, time.Now(), stdout)
	default:
		usage(stderr)
		return 2
	}
	if err != nil {
		log.Error("before-yes: "+cmd, "err", err)
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: before-yes serve|check|config|seed-cards|backup|usage [-config file] [-age N] [-dir demo-cards] [-keep N] [-days N] [title]")
}

// serve listens until ctx ends.
func serve(ctx context.Context, log *slog.Logger, s Settings) error {
	a, err := NewServeApp(ctx, s, log)
	if err != nil {
		return err
	}
	defer a.Close()
	hs := &http.Server{Addr: s.Listen, Handler: a.Handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	log.Info("listening", "addr", s.Listen, "reader", s.Reader.Model, "judge", s.Judge.Model, "web", a.WebServed)
	if err := hs.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// options are a command's flags and its title.
type options struct {
	config string
	age    int
	dir    string
	keep   int
	days   int
	title  string
}

// parseArgs reads the flags anywhere among the arguments: before, after
// or between the words of the title, so `check Roblox -age 12` and
// `check -age 12 Roblox` are the same check.
func parseArgs(cmd string, rest []string, stderr io.Writer) (options, bool) {
	var o options
	fl := goflag.NewFlagSet(cmd, goflag.ContinueOnError)
	fl.SetOutput(stderr)
	fl.StringVar(&o.config, "config", "", "settings file (default: $BEFORE_YES_CONFIG, then ./before-yes.toml)")
	fl.IntVar(&o.age, "age", 9, "the child's age (check)")
	fl.StringVar(&o.dir, "dir", "testdata/demo-cards", "the card files to store (seed-cards)")
	fl.IntVar(&o.keep, "keep", 14, "copies to keep of each database (backup)")
	fl.IntVar(&o.days, "days", 14, "UTC days to report, newest first (usage)")
	var words []string
	for {
		if err := fl.Parse(rest); err != nil {
			return o, false
		}
		if fl.NArg() == 0 {
			break
		}
		words = append(words, fl.Arg(0))
		rest = fl.Args()[1:]
	}
	o.title = strings.Join(words, " ")
	return o, true
}
