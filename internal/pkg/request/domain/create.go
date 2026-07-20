package domainrequest

type CreateDomainRequest struct {
	Hostname  string `json:"hostname" binding:"required"`
	OriginURL string `json:"originUrl" binding:"required"`
}
