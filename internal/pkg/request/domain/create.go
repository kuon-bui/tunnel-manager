package domainrequest

type CreateDomainRequest struct {
	Hostname string       `json:"hostname" binding:"required"`
	ZoneID   string       `json:"zoneId" binding:"required"`
	Routes   []RouteInput `json:"routes" binding:"required,min=1,max=50,dive"`
}
