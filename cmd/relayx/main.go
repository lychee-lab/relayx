package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lychee-lab/relayx/internal/app"
	"github.com/lychee-lab/relayx/internal/codex"
	"github.com/lychee-lab/relayx/internal/config"
	"github.com/lychee-lab/relayx/internal/core"
	"github.com/lychee-lab/relayx/internal/feishu"
	"github.com/lychee-lab/relayx/internal/httpapi"
	"github.com/lychee-lab/relayx/internal/persist"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}

	switch args[0] {
	case "check":
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		return printJSON(cfg.Summary())
	case "parse":
		if len(args) < 2 {
			return errors.New("parse requires a message argument")
		}
		cmd, err := core.ParseCommand(args[1])
		if err != nil {
			return err
		}
		return printJSON(cmd)
	case "serve":
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		return serve(cfg)
	case "help", "-h", "--help":
		return usage()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() error {
	fmt.Println(`relayx

Usage:
  relayx check
  relayx parse "/codex start repo=/tmp/demo fix the test"
  relayx serve`)
	return nil
}

func serve(cfg config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var codexAdapter codex.Adapter
	if cfg.CodexMode == "app-server" {
		adapter, err := codex.StartAppServer(ctx, codex.ProcessOptions{Bin: cfg.CodexBin})
		if err != nil {
			return err
		}
		initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := adapter.Initialize(initCtx); err != nil {
			cancel()
			_ = adapter.Close()
			return err
		}
		cancel()
		defer adapter.Close()
		codexAdapter = adapter
	}

	var notifier app.Notifier
	var feishuClient *feishu.Client
	if cfg.FeishuConfigured {
		feishuClient = &feishu.Client{
			AppID:     cfg.FeishuAppID,
			AppSecret: cfg.FeishuAppSecret,
			BaseURL:   cfg.FeishuBaseURL,
		}
		notifier = feishu.Notifier{Client: feishuClient}
	}

	stateStore := &persist.FileStateStore{Path: cfg.DBPath}
	snapshot, err := stateStore.Load(ctx)
	if err != nil {
		return err
	}

	service := app.NewService(
		core.NewTaskManagerFromSnapshot(snapshot),
		app.WithCodex(codexAdapter),
		app.WithNotifier(notifier),
		app.WithPolicy(core.Policy{
			AuthorizedUsers: cfg.AuthorizedUsers,
			AllowedRepos:    cfg.AllowedRepos,
		}),
		app.WithStateStore(stateStore),
		app.WithAuditor(&persist.FileAuditor{Path: cfg.AuditPath}),
	)
	go service.Run(ctx)

	if cfg.FeishuConfigured && feishuLongConnectionEnabled(cfg.FeishuReceiveMode) {
		receiver := feishu.WSReceiver{
			AppID:             cfg.FeishuAppID,
			AppSecret:         cfg.FeishuAppSecret,
			BaseURL:           cfg.FeishuBaseURL,
			VerificationToken: cfg.FeishuVerifyToken,
			Service:           service,
			Notifier:          notifier,
		}
		go func() {
			if err := receiver.Start(ctx); err != nil {
				log.Printf("feishu long connection receiver stopped: %v", err)
			}
		}()
	} else if cfg.FeishuConfigured {
		log.Printf("feishu long connection receiver disabled receive_mode=%s", cfg.FeishuReceiveMode)
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           httpapi.NewHandler(service, notifier, cfg.FeishuVerifyToken),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		log.Printf("relayx dev server listening on %s", cfg.ListenAddr)
		errs <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func feishuLongConnectionEnabled(mode string) bool {
	switch mode {
	case "long_connection", "both":
		return true
	default:
		return false
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
