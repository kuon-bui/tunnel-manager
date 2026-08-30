package domainrequest

type RouteInput struct {
	Path        string `json:"path" binding:"required"`
	OriginURL   string `json:"originUrl" binding:"required"`
	StripPrefix bool   `json:"stripPrefix"`
}

type ReplaceRoutesRequest struct {
	Routes []RouteInput `json:"routes" binding:"required,min=1,max=50,dive"`
}
