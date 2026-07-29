package domainroute

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/config"
	domainrequest "tunnelmanager/internal/pkg/request/domain"
	authservice "tunnelmanager/internal/services/auth"
	domainservice "tunnelmanager/internal/services/domain"

	"github.com/gin-gonic/gin"
)

type fakeRouteDomainService struct {
	domainservice.DomainService
	zones    []model.CloudflareZone
	zonesErr error
	created  *model.Domain
	domains  []*model.Domain
}

func (f *fakeRouteDomainService) ListCloudflareZones(context.Context) ([]model.CloudflareZone, error) {
	return f.zones, f.zonesErr
}

func (f *fakeRouteDomainService) CreateDomain(context.Context, string, string, string) (*model.Domain, error) {
	return f.created, f.zonesErr
}

func (f *fakeRouteDomainService) GetDomain(context.Context, string) (*model.Domain, error) {
	return f.domains[0], nil
}

func (f *fakeRouteDomainService) ListDomains(context.Context, domainrequest.ListDomainRequest) ([]*model.Domain, string, error) {
	return f.domains, "", nil
}

type fakeRouteAuthService struct {
	authservice.AuthService
}

func (f *fakeRouteAuthService) Authenticate(context.Context, string) (string, error) {
	return "admin", nil
}

func TestZoneListRequiresJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handler := &DomainHandler{domainService: &fakeRouteDomainService{}}
	route := &DomainRoute{
		Engine:        engine,
		domainHandler: handler,
		authService:   &fakeRouteAuthService{},
		cfg:           config.Config{},
	}
	route.Setup()

	request := httptest.NewRequest(http.MethodGet, "/api/cloudflare/zones", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestDomainDetailStreamRequiresJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	route := &DomainRoute{
		Engine:        engine,
		domainHandler: &DomainHandler{domainService: &fakeRouteDomainService{}},
		authService:   &fakeRouteAuthService{},
		cfg:           config.Config{},
	}
	route.Setup()

	request := httptest.NewRequest(http.MethodGet, "/api/domains/domain-1/stream", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestListCloudflareZonesResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handler := &DomainHandler{domainService: &fakeRouteDomainService{zones: []model.CloudflareZone{{ID: "zone-1", Name: "example.com", Status: "active"}}}}
	engine.GET("/api/cloudflare/zones", handler.listCloudflareZones)

	request := httptest.NewRequest(http.MethodGet, "/api/cloudflare/zones", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"items":[{"id":"zone-1","name":"example.com","status":"active"}]`) {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
}

func TestListCloudflareZonesHidesUpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handler := &DomainHandler{domainService: &fakeRouteDomainService{zonesErr: errors.New("token-secret upstream failure")}}
	engine.GET("/api/cloudflare/zones", handler.listCloudflareZones)

	request := httptest.NewRequest(http.MethodGet, "/api/cloudflare/zones", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "token-secret") {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
}

func TestCreateDomainRequiresZoneIDAndReturnsIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeRouteDomainService{created: &model.Domain{ID: "domain-1", CloudflareZoneID: "zone-1"}}
	engine := gin.New()
	handler := &DomainHandler{domainService: service}
	engine.POST("/api/domains", handler.createDomain)

	missingZone := httptest.NewRequest(http.MethodPost, "/api/domains", bytes.NewBufferString(`{"hostname":"app.example.com","originUrl":"http://localhost:8080"}`))
	missingZone.Header.Set("Content-Type", "application/json")
	missingResponse := httptest.NewRecorder()
	engine.ServeHTTP(missingResponse, missingZone)
	if missingResponse.Code != http.StatusBadRequest {
		t.Fatalf("missing zone status = %d", missingResponse.Code)
	}

	valid := httptest.NewRequest(http.MethodPost, "/api/domains", bytes.NewBufferString(`{"hostname":"app.example.com","originUrl":"http://localhost:8080","zoneId":"zone-1"}`))
	valid.Header.Set("Content-Type", "application/json")
	validResponse := httptest.NewRecorder()
	engine.ServeHTTP(validResponse, valid)
	if validResponse.Code != http.StatusCreated || !strings.Contains(validResponse.Body.String(), `"zoneId":"zone-1"`) {
		t.Fatalf("valid status/body = %d/%s", validResponse.Code, validResponse.Body.String())
	}
}

func TestGetAndListDomainsReturnZoneID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeRouteDomainService{domains: []*model.Domain{{ID: "domain-1", CloudflareZoneID: "zone-1"}}}
	engine := gin.New()
	handler := &DomainHandler{domainService: service}
	engine.GET("/api/domains/:id", handler.getDomain)
	engine.GET("/api/domains", handler.listDomains)

	for _, path := range []string{"/api/domains/domain-1", "/api/domains"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"zoneId":"zone-1"`) {
			t.Fatalf("%s status/body = %d/%s", path, response.Code, response.Body.String())
		}
	}
}

func TestCreateDomainMapsCloudflareFailureToBadGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handler := &DomainHandler{domainService: &fakeRouteDomainService{zonesErr: domainservice.ErrCloudflareUnavailable}}
	engine.POST("/api/domains", handler.createDomain)

	request := httptest.NewRequest(http.MethodPost, "/api/domains", bytes.NewBufferString(`{"hostname":"app.example.com","originUrl":"http://localhost:8080","zoneId":"zone-1"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
}
