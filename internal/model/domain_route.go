package model

import (
	"time"

	"github.com/uptrace/bun"
)

type DomainRoute struct {
	bun.BaseModel `bun:"table:domain_routes,alias:dr"`

	ID          string    `bun:"id,pk" json:"id"`
	DomainID    string    `bun:"domain_id,notnull" json:"-"`
	Path        string    `bun:"path,notnull" json:"path"`
	OriginURL   string    `bun:"origin_url,notnull" json:"originUrl"`
	StripPrefix bool      `bun:"strip_prefix,notnull,default:false" json:"stripPrefix"`
	CreatedAt   time.Time `bun:"created_at,notnull" json:"createdAt"`
	UpdatedAt   time.Time `bun:"updated_at,notnull" json:"updatedAt"`
}
