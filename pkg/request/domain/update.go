package domainrequest

type UpdateDomainRequest struct {
	OriginURL string `json:"origin_url" binding:"required"`
}
