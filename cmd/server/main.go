// backend/cmd/server/main.go
package main

import (
	"log"

	"go.uber.org/fx"

	"tunnelmanager/internal/module"
)

func main() {
	app := fx.New(module.App)
	if err := app.Err(); err != nil {
		log.Fatalf("app: %v", err)
	}
	app.Run()
}
