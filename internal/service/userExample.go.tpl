package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jinzhu/copier"
	"google.golang.org/grpc"

	"github.com/18721889353/sunshine/pkg/sgorm/query"
	"github.com/18721889353/sunshine/pkg/grpc/interceptor"
	"github.com/18721889353/sunshine/pkg/logger"

	serverNameExampleV1 "github.com/18721889353/sunshine/api/serverNameExample/v1"
	"github.com/18721889353/sunshine/internal/cache"
	"github.com/18721889353/sunshine/internal/dao"
	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/internal/ecode"
	"github.com/18721889353/sunshine/internal/model"
)

func init() {
	registerFns = append(registerFns, func(server *grpc.Server) {
		serverNameExampleV1.Register{{.TableNameCamel}}Server(server, New{{.TableNameCamel}}Server()) // register service to the rpc service
	})
}

var _ serverNameExampleV1.{{.TableNameCamel}}Server = (*{{.TableNameCamelFCL}})(nil)
var _ time.Time

type {{.TableNameCamelFCL}} struct {
	serverNameExampleV1.Unimplemented{{.TableNameCamel}}Server

	iDao dao.{{.TableNameCamel}}Dao
}

// New{{.TableNameCamel}}Server create a new service
func New{{.TableNameCamel}}Server() serverNameExampleV1.{{.TableNameCamel}}Server {
	return &{{.TableNameCamelFCL}}{
		iDao: dao.New{{.TableNameCamel}}Dao(
			database.GetDB(), // todo show db driver name here
			cache.New{{.TableNameCamel}}Cache(database.GetCacheType()),
		),
	}
}

// Create a record
func (s *{{.TableNameCamelFCL}}) Create(ctx context.Context, req *serverNameExampleV1.Create{{.TableNameCamel}}Request) (*serverNameExampleV1.Create{{.TableNameCamel}}Reply, error) {
	err := req.Validate()
	if err != nil {
		logger.WarnWithCtx(ctx, "req.Validate error", logger.Err(err), logger.Any("req", req))
		return nil, ecode.StatusInvalidParams.Err()
	}
	ctx = interceptor.WrapServerCtx(ctx)

	record := &model.{{.TableNameCamel}}{}
	err = copier.Copy(record, req)
	if err != nil {
		return nil, ecode.StatusCreate{{.TableNameCamel}}.Err()
	}
	// Note: if copier.Copy cannot assign a value to a field, add it here

	err = s.iDao.Create(ctx, record)
	if err != nil {
		logger.ErrorWithCtx(ctx, "Create error", logger.Err(err), logger.Any("{{.TableNameCamelFCL}}", record))
		return nil, ecode.StatusInternalServerError.ToRPCErr()
	}

	return &serverNameExampleV1.Create{{.TableNameCamel}}Reply{ {{if .IsStandardPrimaryKey}}Id{{else}}{{.ColumnNameCamel}}{{end}}: record.{{.ColumnNameCamel}} }, nil
}

// DeleteBy{{.ColumnNameCamel}} delete a record by {{.ColumnNameCamelFCL}}
func (s *{{.TableNameCamelFCL}}) DeleteBy{{.ColumnNameCamel}}(ctx context.Context, req *serverNameExampleV1.Delete{{.TableNameCamel}}By{{.ColumnNameCamel}}Request) (*serverNameExampleV1.Delete{{.TableNameCamel}}By{{.ColumnNameCamel}}Reply, error) {
	err := req.Validate()
	if err != nil {
		logger.WarnWithCtx(ctx, "req.Validate error", logger.Err(err), logger.Any("req", req))
		return nil, ecode.StatusInvalidParams.Err()
	}
	ctx = interceptor.WrapServerCtx(ctx)

	err = s.iDao.DeleteBy{{.ColumnNameCamel}}(ctx, req.{{if .IsStandardPrimaryKey}}Id{{else}}{{.ColumnNameCamel}}{{end}})
	if err != nil {
		logger.ErrorWithCtx(ctx, "DeleteBy{{.ColumnNameCamel}} error", logger.Err(err), logger.Any("{{.ColumnNameCamelFCL}}", req.{{if .IsStandardPrimaryKey}}Id{{else}}{{.ColumnNameCamel}}{{end}}))
		return nil, ecode.StatusInternalServerError.ToRPCErr()
	}

	return &serverNameExampleV1.Delete{{.TableNameCamel}}By{{.ColumnNameCamel}}Reply{}, nil
}

// UpdateBy{{.ColumnNameCamel}} update a record by {{.ColumnNameCamelFCL}}
func (s *{{.TableNameCamelFCL}}) UpdateBy{{.ColumnNameCamel}}(ctx context.Context, req *serverNameExampleV1.Update{{.TableNameCamel}}By{{.ColumnNameCamel}}Request) (*serverNameExampleV1.Update{{.TableNameCamel}}By{{.ColumnNameCamel}}Reply, error) {
	err := req.Validate()
	if err != nil {
		logger.WarnWithCtx(ctx, "req.Validate error", logger.Err(err), logger.Any("req", req))
		return nil, ecode.StatusInvalidParams.Err()
	}
	ctx = interceptor.WrapServerCtx(ctx)

	record := &model.{{.TableNameCamel}}{}
	err = copier.Copy(record, req)
	if err != nil {
		return nil, ecode.StatusUpdateBy{{.ColumnNameCamel}}{{.TableNameCamel}}.Err()
	}
	// Note: if copier.Copy cannot assign a value to a field, add it here
	record.{{.ColumnNameCamel}} = req.{{if .IsStandardPrimaryKey}}Id{{else}}{{.ColumnNameCamel}}{{end}}

	err = s.iDao.UpdateBy{{.ColumnNameCamel}}(ctx, record)
	if err != nil {
		logger.ErrorWithCtx(ctx, "UpdateBy{{.ColumnNameCamel}} error", logger.Err(err), logger.Any("{{.TableNameCamelFCL}}", record))
		return nil, ecode.StatusInternalServerError.ToRPCErr()
	}

	return &serverNameExampleV1.Update{{.TableNameCamel}}By{{.ColumnNameCamel}}Reply{}, nil
}

// GetBy{{.ColumnNameCamel}} get a record by {{.ColumnNameCamelFCL}}
func (s *{{.TableNameCamelFCL}}) GetBy{{.ColumnNameCamel}}(ctx context.Context, req *serverNameExampleV1.Get{{.TableNameCamel}}By{{.ColumnNameCamel}}Request) (*serverNameExampleV1.Get{{.TableNameCamel}}By{{.ColumnNameCamel}}Reply, error) {
	err := req.Validate()
	if err != nil {
		logger.WarnWithCtx(ctx, "req.Validate error", logger.Err(err), logger.Any("req", req))
		return nil, ecode.StatusInvalidParams.Err()
	}
	ctx = interceptor.WrapServerCtx(ctx)

	record, err := s.iDao.GetBy{{.ColumnNameCamel}}(ctx, req.{{if .IsStandardPrimaryKey}}Id{{else}}{{.ColumnNameCamel}}{{end}})
	if err != nil {
		if errors.Is(err, database.ErrRecordNotFound) {
			logger.WarnWithCtx(ctx, "GetBy{{.ColumnNameCamel}} error", logger.Err(err), logger.Any("{{.ColumnNameCamelFCL}}", req.{{if .IsStandardPrimaryKey}}Id{{else}}{{.ColumnNameCamel}}{{end}}))
			return nil, ecode.StatusNotFound.Err()
		}
		logger.ErrorWithCtx(ctx, "GetBy{{.ColumnNameCamel}} error", logger.Err(err), logger.Any("{{.ColumnNameCamelFCL}}", req.{{if .IsStandardPrimaryKey}}Id{{else}}{{.ColumnNameCamel}}{{end}}))
		return nil, ecode.StatusInternalServerError.ToRPCErr()
	}

	data, err := convert{{.TableNameCamel}}(record)
	if err != nil {
		logger.WarnWithCtx(ctx, "convert{{.TableNameCamel}} error", logger.Err(err), logger.Any("{{.TableNameCamelFCL}}", record))
		return nil, ecode.StatusGetBy{{.ColumnNameCamel}}{{.TableNameCamel}}.Err()
	}

	return &serverNameExampleV1.Get{{.TableNameCamel}}By{{.ColumnNameCamel}}Reply{ {{.TableNameCamel}}: data}, nil
}

// List of records by query parameters
func (s *{{.TableNameCamelFCL}}) List(ctx context.Context, req *serverNameExampleV1.List{{.TableNameCamel}}Request) (*serverNameExampleV1.List{{.TableNameCamel}}Reply, error) {
	err := req.Validate()
	if err != nil {
		logger.WarnWithCtx(ctx, "req.Validate error", logger.Err(err), logger.Any("req", req))
		return nil, ecode.StatusInvalidParams.Err()
	}
	ctx = interceptor.WrapServerCtx(ctx)

	params := &query.Params{}
	err = copier.Copy(params, req.Params)
	if err != nil {
		return nil, ecode.StatusList{{.TableNameCamel}}.Err()
	}
	// Note: if copier.Copy cannot assign a value to a field, add it here

	records, total, err := s.iDao.GetByColumns(ctx, params)
	if err != nil {
		if strings.Contains(err.Error(), "query params error:") {
			logger.WarnWithCtx(ctx, "GetByColumns error", logger.Err(err), logger.Any("params", params))
			return nil, ecode.StatusInvalidParams.Err()
		}
		logger.ErrorWithCtx(ctx, "GetByColumns error", logger.Err(err), logger.Any("params", params))
		return nil, ecode.StatusInternalServerError.ToRPCErr()
	}

	{{.TableNamePluralCamelFCL}} := []*serverNameExampleV1.{{.TableNameCamel}}{}
	for _, record := range records {
		data, err := convert{{.TableNameCamel}}(record)
		if err != nil {
			logger.WarnWithCtx(ctx, "convert{{.TableNameCamel}} error", logger.Err(err), logger.Any("{{.ColumnNameCamelFCL}}", record.{{.ColumnNameCamel}}))
			continue
		}
		{{.TableNamePluralCamelFCL}} = append({{.TableNamePluralCamelFCL}}, data)
	}

	return &serverNameExampleV1.List{{.TableNameCamel}}Reply{
		Total:        total,
		{{.TableNamePluralCamel}}: {{.TableNamePluralCamelFCL}},
	}, nil
}

func convert{{.TableNameCamel}}(record *model.{{.TableNameCamel}}) (*serverNameExampleV1.{{.TableNameCamel}}, error) {
	value := &serverNameExampleV1.{{.TableNameCamel}}{}
	err := copier.Copy(value, record)
	if err != nil {
		return nil, err
	}
	// Note: if copier.Copy cannot assign a value to a field, add it here, e.g. CreatedAt, UpdatedAt
	value.{{if .IsStandardPrimaryKey}}Id{{else}}{{.ColumnNameCamel}}{{end}} = record.{{.ColumnNameCamel}}
	// todo generate the conversion createdAt and updatedAt code here
	// delete the templates code start
	value.CreatedAt = record.CreatedAt.Format(time.RFC3339)
	value.UpdatedAt = record.UpdatedAt.Format(time.RFC3339)
	// delete the templates code end
	return value, nil
}
