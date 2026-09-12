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

type CreateApplicationCredentialRequest struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Scopes        []string `json:"scopes"`
	ExpiresInDays *int     `json:"expires_in_days"`
}

type UpdateApplicationCredentialRequest struct {
	Name          *string   `json:"name"`
	Description   *string   `json:"description"`
	Scopes        *[]string `json:"scopes"`
	ExpiresInDays *int      `json:"expires_in_days"`
	Status        *string   `json:"status"`
}

type ApplicationCredentialView struct {
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

type IssuedApplicationCredential struct {
	Application ApplicationCredentialView `json:"application"`
	SecretKey   string                    `json:"secret_key"`
}

type ListApplicationCredentialsResponse struct {
	Items      []ApplicationCredentialView `json:"items"`
	NextCursor string                      `json:"next_cursor"`
	HasMore    bool                        `json:"has_more"`
}

func CreateApplicationCredential(ctx context.Context, actor *utils.Principal, request CreateApplicationCredentialRequest) (*IssuedApplicationCredential, error) {
	ownerID, err := requireApplicationCredentialOwner(actor)
	if err != nil {
		return nil, err
	}
	applicationService, err := applicationCredentialServiceOrError()
	if err != nil {
		return nil, err
	}
	application, secret, err := applicationService.Create(ctx, ownerID, service.CreateInput{
		Name: request.Name, Description: request.Description, Scopes: request.Scopes, ExpiresInDays: request.ExpiresInDays,
	})
	if err != nil {
		return nil, err
	}
	return &IssuedApplicationCredential{Application: buildApplicationCredentialView(application), SecretKey: secret}, nil
}

func ListApplicationCredentials(ctx context.Context, actor *utils.Principal, cursor int64, limit int) (*ListApplicationCredentialsResponse, error) {
	ownerID, err := requireApplicationCredentialOwner(actor)
	if err != nil {
		return nil, err
	}
	applicationService, err := applicationCredentialServiceOrError()
	if err != nil {
		return nil, err
	}
	applications, hasMore, err := applicationService.List(ctx, ownerID, cursor, limit)
	if err != nil {
		return nil, err
	}
	response := &ListApplicationCredentialsResponse{Items: make([]ApplicationCredentialView, 0, len(applications)), HasMore: hasMore}
	for _, application := range applications {
		response.Items = append(response.Items, buildApplicationCredentialView(application))
	}
	if hasMore && len(applications) > 0 {
		response.NextCursor = strconv.FormatInt(applications[len(applications)-1].ID, 10)
	}
	return response, nil
}

func GetApplicationCredential(ctx context.Context, actor *utils.Principal, applicationID int64) (*ApplicationCredentialView, error) {
	ownerID, err := requireApplicationCredentialOwner(actor)
	if err != nil {
		return nil, err
	}
	applicationService, err := applicationCredentialServiceOrError()
	if err != nil {
		return nil, err
	}
	application, err := applicationService.Get(ctx, ownerID, applicationID)
	if err != nil {
		return nil, err
	}
	view := buildApplicationCredentialView(application)
	return &view, nil
}

func UpdateApplicationCredential(ctx context.Context, actor *utils.Principal, applicationID, revision int64, request UpdateApplicationCredentialRequest) (*ApplicationCredentialView, error) {
	ownerID, err := requireApplicationCredentialOwner(actor)
	if err != nil {
		return nil, err
	}
	applicationService, err := applicationCredentialServiceOrError()
	if err != nil {
		return nil, err
	}
	application, err := applicationService.Update(ctx, ownerID, applicationID, revision, service.UpdateInput{
		Name: request.Name, Description: request.Description, Scopes: request.Scopes, ExpiresInDays: request.ExpiresInDays, Status: request.Status,
	})
	if err != nil {
		return nil, err
	}
	view := buildApplicationCredentialView(application)
	return &view, nil
}

func RotateApplicationSecret(ctx context.Context, actor *utils.Principal, applicationID, revision int64) (*IssuedApplicationCredential, error) {
	ownerID, err := requireApplicationCredentialOwner(actor)
	if err != nil {
		return nil, err
	}
	applicationService, err := applicationCredentialServiceOrError()
	if err != nil {
		return nil, err
	}
	application, secret, err := applicationService.Rotate(ctx, ownerID, applicationID, revision)
	if err != nil {
		return nil, err
	}
	return &IssuedApplicationCredential{Application: buildApplicationCredentialView(application), SecretKey: secret}, nil
}

func RevokeApplicationCredential(ctx context.Context, actor *utils.Principal, applicationID, revision int64) (*ApplicationCredentialView, error) {
	ownerID, err := requireApplicationCredentialOwner(actor)
	if err != nil {
		return nil, err
	}
	applicationService, err := applicationCredentialServiceOrError()
	if err != nil {
		return nil, err
	}
	application, err := applicationService.Revoke(ctx, ownerID, applicationID, revision)
	if err != nil {
		return nil, err
	}
	view := buildApplicationCredentialView(application)
	return &view, nil
}

func requireApplicationCredentialOwner(actor *utils.Principal) (int64, error) {
	if actor == nil || actor.UserID <= 0 {
		return 0, appErrors.ErrUnauthorized
	}
	if actor.Kind != "user" {
		return 0, appErrors.ErrForbidden
	}
	return actor.UserID, nil
}

func applicationCredentialServiceOrError() (*service.Service, error) {
	applicationService := service.Default()
	if applicationService == nil {
		return nil, appErrors.ErrDependency
	}
	return applicationService, nil
}

func buildApplicationCredentialView(application *model.Application) ApplicationCredentialView {
	return ApplicationCredentialView{
		ID: strconv.FormatInt(application.ID, 10), Name: application.Name, Description: application.Description,
		AccessKey: application.AccessKey, Scopes: append([]string(nil), application.Scopes...), Status: applicationCredentialStatusName(application.Status),
		Revision: application.Revision, SecretVersion: application.SecretVersion, CreateTime: application.CreateTime,
		UpdateTime: application.UpdateTime, ExpiresAt: application.ExpiresAt, LastIssuedAt: application.LastIssuedAt,
	}
}

func applicationCredentialStatusName(status int) string {
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
