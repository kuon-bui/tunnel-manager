package lifecycle

import (
	"context"
	"log"
	"net"
	stdhttp "net/http"
	"os"
	"time"

	"go.uber.org/fx"

	"tunnelmanager/internal/pkg/common"
	appconfig "tunnelmanager/internal/pkg/config"
)

type Reconciler interface {
	Reconcile(ctx context.Context) error
}

type Bootstrapper interface {
	Bootstrap(ctx context.Context) error
}

type LifecycleRunner struct {
	cfg           appconfig.Config
	service       Reconciler
	bootstrappers []Bootstrapper `group:"bootstrappers"`
	server        *stdhttp.Server
	mkdirAll      func(string, os.FileMode) error
	chmod         func(string, os.FileMode) error
	startServer   func(*stdhttp.Server) error
	routes        []common.Route `group:"routes"`
}

type LifecycleParams struct {
	fx.In

	Cfg           appconfig.Config
	Service       Reconciler
	Bootstrappers []Bootstrapper `group:"bootstrappers"`
	Server        *stdhttp.Server
	Routes        []common.Route `group:"routes"`
}

func NewLifecycleRunner(params LifecycleParams) *LifecycleRunner {
	return &LifecycleRunner{
		cfg:           params.Cfg,
		service:       params.Service,
		bootstrappers: params.Bootstrappers,
		server:        params.Server,
		routes:        params.Routes,
		mkdirAll:      os.MkdirAll,
		chmod:         os.Chmod,
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
			for _, b := range r.bootstrappers {
				if err := b.Bootstrap(ctx); err != nil {
					return err
				}
			}

			if err := r.mkdirAll(r.cfg.LogDir, 0o755); err != nil {
				return err
			}
			if err := r.chmod(r.cfg.DBPath, 0o600); err != nil {
				return err
			}
			if err := r.service.Reconcile(ctx); err != nil {
				log.Printf("reconcile: %v", err)
			}

			for _, route := range r.routes {
				route.Setup()
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

var Module = fx.Module("lifecycle",
	fx.Provide(NewLifecycleRunner),
	fx.Invoke(func(lc fx.Lifecycle, runner *LifecycleRunner) {
		runner.Register(lc)
	}),
)
