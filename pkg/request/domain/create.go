package domainrequest

type CreateDomainRequest struct {
	Hostname  string `json:"hostname" binding:"required"`
	OriginURL string `json:"origin_url" binding:"required"`
}
