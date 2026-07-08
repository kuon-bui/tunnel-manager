package domainrequest

import (
	"tunnelmanager/internal/pkg/common"
	"tunnelmanager/internal/pkg/constant"
)

type ListDomainRequest struct {
	common.Pagination

	Status   constant.DomainStatus `form:"status"`
	Hostname string                `form:"hostname"`
}
