package handler

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jinzhu/copier"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/sgorm/query"

	serverNameExampleV1 "github.com/18721889353/sunshine/api/serverNameExample/v1"
	"github.com/18721889353/sunshine/internal/cache"
	"github.com/18721889353/sunshine/internal/dao"
	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/internal/ecode"
	"github.com/18721889353/sunshine/internal/model"
)

var _ serverNameExampleV1.UserExampleLogicer = (*userExamplePbHandler)(nil)
var _ time.Time

type userExamplePbHandler struct {
	userExampleDao *dao.UserExampleDao
}

// NewUserExamplePbHandler create a handler
func NewUserExamplePbHandler() serverNameExampleV1.UserExampleLogicer {
	return &userExamplePbHandler{
		userExampleDao: dao.NewUserExampleDao(
			database.GetDB(), // todo show db driver name here
			cache.NewUserExampleCache(database.GetCacheType()),
		),
	}
}

// Create a record
func (h *userExamplePbHandler) Create(ctx context.Context, req *serverNameExampleV1.CreateUserExampleRequest) (*serverNameExampleV1.CreateUserExampleReply, error) {
	err := req.Validate()
	if err != nil {
		logger.WarnWithCtx(ctx, "req.Validate error", logger.Err(err), logger.Any("req", req))
		return nil, ecode.InvalidParams.Err()
	}

	userExample := &model.UserExample{}
	err = copier.Copy(userExample, req)
	if err != nil {
		return nil, ecode.ErrCreateUserExample.Err()
	}
	// Note: if copier.Copy cannot assign a value to a field, add it here

	err = h.userExampleDao.Create(ctx, userExample)
	if err != nil {
		logger.ErrorWithCtx(ctx, "Create error", logger.Err(err), logger.Any("userExample", userExample))
		return nil, ecode.InternalServerError.Err()
	}

	return &serverNameExampleV1.CreateUserExampleReply{Id: userExample.ID}, nil
}

// DeleteByID delete a record by id
func (h *userExamplePbHandler) DeleteByID(ctx context.Context, req *serverNameExampleV1.DeleteUserExampleByIDRequest) (*serverNameExampleV1.DeleteUserExampleByIDReply, error) {
	err := req.Validate()
	if err != nil {
		logger.WarnWithCtx(ctx, "req.Validate error", logger.Err(err), logger.Any("req", req))
		return nil, ecode.InvalidParams.Err()
	}

	err = h.userExampleDao.DeleteByID(ctx, req.Id)
	if err != nil {
		logger.WarnWithCtx(ctx, "DeleteByID error", logger.Err(err))
		return nil, ecode.InternalServerError.Err()
	}

	return &serverNameExampleV1.DeleteUserExampleByIDReply{}, nil
}

// UpdateByID update a record by id
func (h *userExamplePbHandler) UpdateByID(ctx context.Context, req *serverNameExampleV1.UpdateUserExampleByIDRequest) (*serverNameExampleV1.UpdateUserExampleByIDReply, error) {
	err := req.Validate()
	if err != nil {
		logger.WarnWithCtx(ctx, "req.Validate error", logger.Err(err), logger.Any("req", req))
		return nil, ecode.InvalidParams.Err()
	}

	userExample := &model.UserExample{}
	err = copier.Copy(userExample, req)
	if err != nil {
		return nil, ecode.ErrUpdateByIDUserExample.Err()
	}
	// Note: if copier.Copy cannot assign a value to a field, add it here
	userExample.ID = req.Id

	err = h.userExampleDao.UpdateByID(ctx, userExample)
	if err != nil {
		logger.ErrorWithCtx(ctx, "UpdateByID error", logger.Err(err), logger.Any("userExample", userExample))
		return nil, ecode.InternalServerError.Err()
	}

	return &serverNameExampleV1.UpdateUserExampleByIDReply{}, nil
}

// GetByID get a record by id
func (h *userExamplePbHandler) GetByID(ctx context.Context, req *serverNameExampleV1.GetUserExampleByIDRequest) (*serverNameExampleV1.GetUserExampleByIDReply, error) {
	err := req.Validate()
	if err != nil {
		logger.WarnWithCtx(ctx, "req.Validate error", logger.Err(err), logger.Any("req", req))
		return nil, ecode.InvalidParams.Err()
	}

	record, err := h.userExampleDao.GetByID(ctx, req.Id)
	if err != nil {
		if errors.Is(err, database.ErrRecordNotFound) {
			logger.WarnWithCtx(ctx, "GetByID error", logger.Err(err), logger.Any("id", req.Id))
			return nil, ecode.NotFound.Err()
		}
		logger.ErrorWithCtx(ctx, "GetByID error", logger.Err(err), logger.Any("id", req.Id))
		return nil, ecode.InternalServerError.Err()
	}

	data, err := convertUserExamplePb(record)
	if err != nil {
		logger.WarnWithCtx(ctx, "convertUserExample error", logger.Err(err), logger.Any("userExample", record))
		return nil, ecode.ErrGetByIDUserExample.Err()
	}

	return &serverNameExampleV1.GetUserExampleByIDReply{
		UserExample: data,
	}, nil
}

// List of records by query parameters
func (h *userExamplePbHandler) List(ctx context.Context, req *serverNameExampleV1.ListUserExampleRequest) (*serverNameExampleV1.ListUserExampleReply, error) {
	err := req.Validate()
	if err != nil {
		logger.WarnWithCtx(ctx, "req.Validate error", logger.Err(err), logger.Any("req", req))
		return nil, ecode.InvalidParams.Err()
	}

	params := &query.Params{}
	err = copier.Copy(params, req.Params)
	if err != nil {
		return nil, ecode.ErrListUserExample.Err()
	}
	// Note: if copier.Copy cannot assign a value to a field, add it here

	records, total, err := h.userExampleDao.GetByColumns(ctx, params)
	if err != nil {
		if strings.Contains(err.Error(), "query params error:") {
			logger.WarnWithCtx(ctx, "GetByColumns error", logger.Err(err), logger.Any("params", params))
			return nil, ecode.InvalidParams.Err()
		}
		logger.ErrorWithCtx(ctx, "GetByColumns error", logger.Err(err), logger.Any("params", params))
		return nil, ecode.InternalServerError.Err()
	}

	userExamples := []*serverNameExampleV1.UserExample{}
	for _, record := range records {
		data, err := convertUserExamplePb(record)
		if err != nil {
			logger.WarnWithCtx(ctx, "convertUserExample error", logger.Err(err), logger.Any("id", record.ID))
			continue
		}
		userExamples = append(userExamples, data)
	}

	return &serverNameExampleV1.ListUserExampleReply{
		Total:        total,
		UserExamples: userExamples,
	}, nil
}

func convertUserExamplePb(record *model.UserExample) (*serverNameExampleV1.UserExample, error) {
	value := &serverNameExampleV1.UserExample{}
	err := copier.Copy(value, record)
	if err != nil {
		return nil, err
	}
	// Note: if copier.Copy cannot assign a value to a field, add it here, e.g. CreatedAt, UpdatedAt
	value.Id = record.ID
	// todo generate the conversion createdAt and updatedAt code here
	// delete the templates code start
	value.CreatedAt = record.CreatedAt.Format(time.RFC3339)
	value.UpdatedAt = record.UpdatedAt.Format(time.RFC3339)
	// delete the templates code end
	return value, nil
}
