package scopes

import (
	"encoding/base64"
	"encoding/json"
	"time"
	"tunnelmanager/internal/pkg/common"

	"github.com/uptrace/bun"
)

type paginateData struct {
	Id        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
}

func Paginate(p common.Pagination) func(*bun.SelectQuery) *bun.SelectQuery {
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		if p.PageSize < 0 {
			return q
		}

		if p.Cursor != "" {
			cursorData, err := decodeCursor(p.Cursor)
			if err != nil {
				return q
			}
			q = q.Where("(created_at, id) > (?, ?)", cursorData.CreatedAt, cursorData.Id)
		} else if p.Page > 0 && p.PageSize > 0 {
			q = q.Offset((p.Page - 1) * p.PageSize)
		}

		if p.PageSize > 0 {
			q = q.Limit(p.PageSize)
		}
		return q
	}
}

func decodeCursor(cursor string) (paginateData, error) {
	if cursor == "" {
		return paginateData{}, nil
	}

	var data paginateData
	decoded, err := base64URLDecode(cursor)
	if err != nil {
		return paginateData{}, err
	}
	err = json.Unmarshal(decoded, &data)
	if err != nil {
		return paginateData{}, err
	}

	return data, nil
}

func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func base64URLDecode(encoded string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(encoded)
}

func EncodeCursor(id string, createdAt time.Time) (string, error) {
	data := paginateData{
		Id:        id,
		CreatedAt: createdAt,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	return base64URLEncode(jsonData), nil
}
