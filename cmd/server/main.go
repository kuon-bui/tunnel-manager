// cmd/server/main.go
package main

import (
	"go.uber.org/fx"

	"tunnelmanager/internal/application"
	"tunnelmanager/internal/application/api"
	"tunnelmanager/internal/pkg/logger"
)

func main() {
	app := fx.New(
		application.Module,
		api.Module,
	)
	if err := app.Err(); err != nil {
		logger.Fatalf("app: %s", err)
	}

	app.Run()
}
