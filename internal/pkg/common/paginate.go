package common

type Pagination struct {
	Page     int `form:"page"`
	PageSize int `form:"pageSize"`

	Cursor string `form:"cursor"`
}
