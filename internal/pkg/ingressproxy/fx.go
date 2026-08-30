package ingressproxy

import "go.uber.org/fx"

var Module = fx.Module("ingressproxy", fx.Provide(New))
