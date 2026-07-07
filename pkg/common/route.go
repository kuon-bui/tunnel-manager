package common

import (
	"go.uber.org/fx"
)

type Route interface {
	Setup()
}

func ProvideAsRoute(f any) fx.Option {
	return fx.Provide(
		fx.Annotate(
			f,
			fx.As(new(Route)),
			fx.ResultTags(`group:"routes"`),
		),
	)
}
