package lifecycle

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/config"

	"go.uber.org/fx"
)

type testLifecycle struct {
	hook fx.Hook
}

func (l *testLifecycle) Append(hook fx.Hook) {
	l.hook = hook
}

type failingReconciler struct{}

func (failingReconciler) Reconcile(context.Context) error {
	return errors.New("hydrate failed")
}

type lifecycleProxy struct {
	started  int
	shutdown int
}

func (*lifecycleProxy) URL() string { return "http://127.0.0.1:20080" }
func (p *lifecycleProxy) Start() error {
	p.started++
	return nil
}
func (p *lifecycleProxy) Shutdown(context.Context) error {
	p.shutdown++
	return nil
}
func (*lifecycleProxy) PrepareDomain(string, string, []model.DomainRoute, []model.DomainRoute) error {
	return nil
}
func (*lifecycleProxy) CommitDomain(string, string, []model.DomainRoute) error   { return nil }
func (*lifecycleProxy) RollbackDomain(string, string, []model.DomainRoute) error { return nil }
func (*lifecycleProxy) RemoveDomain(string)                                      {}

func TestStartupShutsDownProxyWhenReconcileFails(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	if err := os.WriteFile(dbPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	proxy := &lifecycleProxy{}
	runner := NewLifecycleRunner(LifecycleParams{
		Cfg:     config.Config{DBPath: dbPath, LogDir: t.TempDir()},
		Service: failingReconciler{},
		Server:  &http.Server{},
		Proxy:   proxy,
	})
	lifecycle := &testLifecycle{}
	runner.Register(lifecycle)

	if err := lifecycle.hook.OnStart(t.Context()); err == nil {
		t.Fatal("expected reconcile failure")
	}
	if proxy.started != 1 || proxy.shutdown != 1 {
		t.Fatalf("proxy start/shutdown = %d/%d", proxy.started, proxy.shutdown)
	}
}
