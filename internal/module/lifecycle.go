package module

import (
	"context"
	"database/sql"
	"log"
	"net"
	stdhttp "net/http"
	"os"
	"time"

	"go.uber.org/fx"

	appconfig "tunnelmanager/internal/infrastructure/config"
)

type Reconciler interface {
	Reconcile(ctx context.Context) error
}

type LifecycleRunner struct {
	cfg         appconfig.Config
	db          *sql.DB
	service     Reconciler
	server      *stdhttp.Server
	mkdirAll    func(string, os.FileMode) error
	chmod       func(string, os.FileMode) error
	startServer func(*stdhttp.Server) error
}

func NewLifecycleRunner(cfg appconfig.Config, db *sql.DB, service Reconciler, server *stdhttp.Server) *LifecycleRunner {
	return &LifecycleRunner{
		cfg:      cfg,
		db:       db,
		service:  service,
		server:   server,
		mkdirAll: os.MkdirAll,
		chmod:    os.Chmod,
		startServer: func(server *stdhttp.Server) error {
			listener, err := net.Listen("tcp", server.Addr)
			if err != nil {
				return err
			}
			go func() {
				if err := server.Serve(listener); err != nil && err != stdhttp.ErrServerClosed {
					log.Printf("listen: %v", err)
				}
			}()
			return nil
		},
	}
}

func (r *LifecycleRunner) Register(lc fx.Lifecycle) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := r.mkdirAll(r.cfg.LogDir, 0o755); err != nil {
				return err
			}
			if err := r.chmod(r.cfg.DBPath, 0o600); err != nil {
				return err
			}
			if err := r.service.Reconcile(ctx); err != nil {
				log.Printf("reconcile: %v", err)
			}
			return r.startServer(r.server)
		},
		OnStop: func(ctx context.Context) error {
			shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return r.server.Shutdown(shutdownCtx)
		},
	})
}

var Lifecycle = fx.Module("lifecycle",
	fx.Provide(NewLifecycleRunner),
	fx.Invoke(func(lc fx.Lifecycle, runner *LifecycleRunner) {
		runner.Register(lc)
	}),
)
