package model

import (
	"time"

	"github.com/uptrace/bun"
)

type Auth struct {
	bun.BaseModel `bun:"table:auths,alias:a"`

	Username     string    `bun:"username,pk" json:"username"`
	Password     string    `bun:"password,notnull" json:"-"`
	TokenVersion int64     `bun:"token_version,notnull,default:1" json:"-"`
	CreatedAt    time.Time `bun:"created_at,notnull" json:"createdAt"`
	UpdatedAt    time.Time `bun:"updated_at,notnull" json:"updatedAt"`
}
