// Command before-yes serves the Before Yes API and app, runs one check from
// the terminal, or prints the settings plan.
//
//	before-yes serve  [-config before-yes.toml]
//	before-yes check  [-config before-yes.toml] [-age 9] Roblox
//	before-yes config [-config before-yes.toml]
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
	cmd, rest := args[0], args[1:]
	fl := goflag.NewFlagSet(cmd, goflag.ContinueOnError)
	fl.SetOutput(stderr)
	cfgPath := fl.String("config", "", "settings file (default: $BEFORE_YES_CONFIG, then ./before-yes.toml)")
	age := fl.Int("age", 9, "the child's age (check)")
	if err := fl.Parse(rest); err != nil {
		return 2
	}
	s, plan, err := Load(ctx, *cfgPath, nil)
	if err != nil {
		log.Error("before-yes: settings", "err", err)
		return 1
	}
	switch cmd {
	case "serve":
		err = serve(ctx, log, s)
	case "check":
		err = check(ctx, log, s, strings.Join(fl.Args(), " "), *age, stdout, stderr)
	case "config":
		_, err = fmt.Fprint(stdout, plan.String())
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
	fmt.Fprintln(w, "usage: before-yes serve|check|config [-config file] [-age N] [title]")
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
