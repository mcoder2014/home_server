package applications

import (
	"context"
	"strconv"
	"time"

	"github.com/mcoder2014/home_server/domain/model"
	service "github.com/mcoder2014/home_server/domain/service/applications"
	appErrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
)

type CreateRequest struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Scopes        []string `json:"scopes"`
	ExpiresInDays *int     `json:"expires_in_days"`
}

type UpdateRequest struct {
	Name          *string   `json:"name"`
	Description   *string   `json:"description"`
	Scopes        *[]string `json:"scopes"`
	ExpiresInDays *int      `json:"expires_in_days"`
	Status        *string   `json:"status"`
}

type ApplicationView struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	AccessKey     string     `json:"access_key"`
	Scopes        []string   `json:"scopes"`
	Status        string     `json:"status"`
	Revision      int64      `json:"revision"`
	SecretVersion int64      `json:"secret_version"`
	CreateTime    time.Time  `json:"create_time"`
	UpdateTime    time.Time  `json:"update_time"`
	ExpiresAt     time.Time  `json:"expires_at"`
	LastIssuedAt  *time.Time `json:"last_issued_at"`
}

type CredentialResponse struct {
	Application ApplicationView `json:"application"`
	SecretKey   string          `json:"secret_key"`
}

type ListResponse struct {
	Items      []ApplicationView `json:"items"`
	NextCursor string            `json:"next_cursor"`
	HasMore    bool              `json:"has_more"`
}

func Create(ctx context.Context, principal *utils.Principal, request CreateRequest) (*CredentialResponse, error) {
	ownerID, err := requireOwner(principal)
	if err != nil {
		return nil, err
	}
	applicationService, err := serviceOrError()
	if err != nil {
		return nil, err
	}
	application, secret, err := applicationService.Create(ctx, ownerID, service.CreateInput{
		Name: request.Name, Description: request.Description, Scopes: request.Scopes, ExpiresInDays: request.ExpiresInDays,
	})
	if err != nil {
		return nil, err
	}
	return &CredentialResponse{Application: newApplicationView(application), SecretKey: secret}, nil
}

func List(ctx context.Context, principal *utils.Principal, cursor int64, limit int) (*ListResponse, error) {
	ownerID, err := requireOwner(principal)
	if err != nil {
		return nil, err
	}
	applicationService, err := serviceOrError()
	if err != nil {
		return nil, err
	}
	applications, hasMore, err := applicationService.List(ctx, ownerID, cursor, limit)
	if err != nil {
		return nil, err
	}
	response := &ListResponse{Items: make([]ApplicationView, 0, len(applications)), HasMore: hasMore}
	for _, application := range applications {
		response.Items = append(response.Items, newApplicationView(application))
	}
	if hasMore && len(applications) > 0 {
		response.NextCursor = strconv.FormatInt(applications[len(applications)-1].ID, 10)
	}
	return response, nil
}

func Get(ctx context.Context, principal *utils.Principal, applicationID int64) (*ApplicationView, error) {
	ownerID, err := requireOwner(principal)
	if err != nil {
		return nil, err
	}
	applicationService, err := serviceOrError()
	if err != nil {
		return nil, err
	}
	application, err := applicationService.Get(ctx, ownerID, applicationID)
	if err != nil {
		return nil, err
	}
	view := newApplicationView(application)
	return &view, nil
}

func Update(ctx context.Context, principal *utils.Principal, applicationID, revision int64, request UpdateRequest) (*ApplicationView, error) {
	ownerID, err := requireOwner(principal)
	if err != nil {
		return nil, err
	}
	applicationService, err := serviceOrError()
	if err != nil {
		return nil, err
	}
	application, err := applicationService.Update(ctx, ownerID, applicationID, revision, service.UpdateInput{
		Name: request.Name, Description: request.Description, Scopes: request.Scopes, ExpiresInDays: request.ExpiresInDays, Status: request.Status,
	})
	if err != nil {
		return nil, err
	}
	view := newApplicationView(application)
	return &view, nil
}

func Rotate(ctx context.Context, principal *utils.Principal, applicationID, revision int64) (*CredentialResponse, error) {
	ownerID, err := requireOwner(principal)
	if err != nil {
		return nil, err
	}
	applicationService, err := serviceOrError()
	if err != nil {
		return nil, err
	}
	application, secret, err := applicationService.Rotate(ctx, ownerID, applicationID, revision)
	if err != nil {
		return nil, err
	}
	return &CredentialResponse{Application: newApplicationView(application), SecretKey: secret}, nil
}

func Revoke(ctx context.Context, principal *utils.Principal, applicationID, revision int64) (*ApplicationView, error) {
	ownerID, err := requireOwner(principal)
	if err != nil {
		return nil, err
	}
	applicationService, err := serviceOrError()
	if err != nil {
		return nil, err
	}
	application, err := applicationService.Revoke(ctx, ownerID, applicationID, revision)
	if err != nil {
		return nil, err
	}
	view := newApplicationView(application)
	return &view, nil
}

func requireOwner(principal *utils.Principal) (int64, error) {
	if principal == nil || principal.UserID <= 0 {
		return 0, appErrors.ErrUnauthorized
	}
	if principal.Kind != "user" {
		return 0, appErrors.ErrForbidden
	}
	return principal.UserID, nil
}

func serviceOrError() (*service.Service, error) {
	applicationService := service.Default()
	if applicationService == nil {
		return nil, appErrors.ErrDependency
	}
	return applicationService, nil
}

func newApplicationView(application *model.Application) ApplicationView {
	return ApplicationView{
		ID: strconv.FormatInt(application.ID, 10), Name: application.Name, Description: application.Description,
		AccessKey: application.AccessKey, Scopes: append([]string(nil), application.Scopes...), Status: statusName(application.Status),
		Revision: application.Revision, SecretVersion: application.SecretVersion, CreateTime: application.CreateTime,
		UpdateTime: application.UpdateTime, ExpiresAt: application.ExpiresAt, LastIssuedAt: application.LastIssuedAt,
	}
}

func statusName(status int) string {
	switch status {
	case model.ApplicationStatusEnabled:
		return service.StatusEnabledName
	case model.ApplicationStatusDisabled:
		return service.StatusDisabledName
	case model.ApplicationStatusRevoked:
		return service.StatusRevokedName
	default:
		return ""
	}
}
