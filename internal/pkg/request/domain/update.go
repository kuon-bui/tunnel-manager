package domainrequest

type UpdateDomainRequest struct {
	OriginURL string `json:"originUrl" binding:"required"`
}
