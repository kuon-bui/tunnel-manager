package model

import "github.com/uptrace/bun"

type Auth struct {
	bun.BaseModel `bun:"table:auths,alias:a"`

	Username string `json:"username" bun:"username,notnull"`
	Password string `json:"password" bun:"password,notnull"`
}
