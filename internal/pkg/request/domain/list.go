package domainrequest

import (
	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/common"
)

type ListDomainRequest struct {
	common.Pagination

	Status   model.Status `form:"status"`
	Hostname string       `form:"hostname"`
}
