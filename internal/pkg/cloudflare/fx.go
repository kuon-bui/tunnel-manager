package cloudflare

import (
	"go.uber.org/fx"
)

var Module = fx.Module("cloudflare",
	fx.Provide(
		NewCloudflareClient,
	),
)
