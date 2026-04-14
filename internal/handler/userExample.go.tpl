package handler

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/copier"

	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/utils"

	"github.com/18721889353/sunshine/internal/cache"
	"github.com/18721889353/sunshine/internal/dao"
	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/internal/ecode"
	"github.com/18721889353/sunshine/internal/model"
	"github.com/18721889353/sunshine/internal/types"
)

var _ {{.TableNameCamel}}Handler = (*{{.TableNameCamelFCL}}Handler)(nil)

// {{.TableNameCamel}}Handler defining the handler interface
type {{.TableNameCamel}}Handler interface {
	Create(c *gin.Context)
	DeleteBy{{.ColumnNameCamel}}(c *gin.Context)
	UpdateBy{{.ColumnNameCamel}}(c *gin.Context)
	GetBy{{.ColumnNameCamel}}(c *gin.Context)
	List(c *gin.Context)
}

type {{.TableNameCamelFCL}}Handler struct {
	iDao dao.{{.TableNameCamel}}Dao
}

// New{{.TableNameCamel}}Handler creating the handler interface
func New{{.TableNameCamel}}Handler() {{.TableNameCamel}}Handler {
	return &{{.TableNameCamelFCL}}Handler{
		iDao: dao.New{{.TableNameCamel}}Dao(
			database.GetDB(), // todo show db driver name here
			cache.New{{.TableNameCamel}}Cache(database.GetCacheType()),
		),
	}
}

// Create a record
// @Summary create {{.TableNameCamelFCL}}
// @Description submit information to create {{.TableNameCamelFCL}}
// @Tags {{.TableNameCamelFCL}}
// @accept json
// @Produce json
// @Param data body types.Create{{.TableNameCamel}}Request true "{{.TableNameCamelFCL}} information"
// @Success 200 {object} types.Create{{.TableNameCamel}}Reply{}
// @Router /api/v1/{{.TableNameCamelFCL}} [post]
// @Security BearerAuth
func (h *{{.TableNameCamelFCL}}Handler) Create(c *gin.Context) {
	form := &types.Create{{.TableNameCamel}}Request{}
	err := c.ShouldBindJSON(form)
	if err != nil {
		logger.WarnWithCtx(middleware.WrapCtx(c), "ShouldBindJSON error: ", logger.Err(err))
		response.Error(c, ecode.InvalidParams)
		return
	}

	{{.TableNameCamelFCL}} := &model.{{.TableNameCamel}}{}
	err = copier.Copy({{.TableNameCamelFCL}}, form)
	if err != nil {
		response.Error(c, ecode.ErrCreate{{.TableNameCamel}})
		return
	}
	// Note: if copier.Copy cannot assign a value to a field, add it here

	ctx := middleware.WrapCtx(c)
	err = h.iDao.Create(ctx, {{.TableNameCamelFCL}})
	if err != nil {
		logger.ErrorWithCtx(middleware.WrapCtx(c), "Create error", logger.Err(err), logger.Any("form", form))
		response.Output(c, ecode.InternalServerError.ToHTTPCode())
		return
	}

	response.Success(c, gin.H{"{{.ColumnNameCamelFCL}}": {{.TableNameCamelFCL}}.{{.ColumnNameCamel}}})
}

// DeleteBy{{.ColumnNameCamel}} delete a record by {{.ColumnNameCamelFCL}}
// @Summary delete {{.TableNameCamelFCL}}
// @Description delete {{.TableNameCamelFCL}} by {{.ColumnNameCamelFCL}}
// @Tags {{.TableNameCamelFCL}}
// @accept json
// @Produce json
// @Param {{.ColumnNameCamelFCL}} path string true "{{.ColumnNameCamelFCL}}"
// @Success 200 {object} types.Delete{{.TableNameCamel}}By{{.ColumnNameCamel}}Reply{}
// @Router /api/v1/{{.TableNameCamelFCL}}/{{{.ColumnNameCamelFCL}}} [delete]
// @Security BearerAuth
func (h *{{.TableNameCamelFCL}}Handler) DeleteBy{{.ColumnNameCamel}}(c *gin.Context) {
	{{.ColumnNameCamelFCL}}, isAbort := get{{.TableNameCamel}}{{.ColumnNameCamel}}FromPath(c)
	if isAbort {
		response.Error(c, ecode.InvalidParams)
		return
	}

	ctx := middleware.WrapCtx(c)
	err := h.iDao.DeleteBy{{.ColumnNameCamel}}(ctx, {{.ColumnNameCamelFCL}})
	if err != nil {
		logger.ErrorWithCtx(middleware.WrapCtx(c), "DeleteBy{{.ColumnNameCamel}} error", logger.Err(err), logger.Any("{{.ColumnNameCamelFCL}}", {{.ColumnNameCamelFCL}}))
		response.Output(c, ecode.InternalServerError.ToHTTPCode())
		return
	}

	response.Success(c)
}

// UpdateBy{{.ColumnNameCamel}} update information by {{.ColumnNameCamelFCL}}
// @Summary update {{.TableNameCamelFCL}}
// @Description update {{.TableNameCamelFCL}} information by {{.ColumnNameCamelFCL}}
// @Tags {{.TableNameCamelFCL}}
// @accept json
// @Produce json
// @Param {{.ColumnNameCamelFCL}} path string true "{{.ColumnNameCamelFCL}}"
// @Param data body types.Update{{.TableNameCamel}}By{{.ColumnNameCamel}}Request true "{{.TableNameCamelFCL}} information"
// @Success 200 {object} types.Update{{.TableNameCamel}}By{{.ColumnNameCamel}}Reply{}
// @Router /api/v1/{{.TableNameCamelFCL}}/{{{.ColumnNameCamelFCL}}} [put]
// @Security BearerAuth
func (h *{{.TableNameCamelFCL}}Handler) UpdateBy{{.ColumnNameCamel}}(c *gin.Context) {
	{{.ColumnNameCamelFCL}}, isAbort := get{{.TableNameCamel}}{{.ColumnNameCamel}}FromPath(c)
	if isAbort {
		response.Error(c, ecode.InvalidParams)
		return
	}

	form := &types.Update{{.TableNameCamel}}By{{.ColumnNameCamel}}Request{}
	err := c.ShouldBindJSON(form)
	if err != nil {
		logger.WarnWithCtx(middleware.WrapCtx(c), "ShouldBindJSON error: ", logger.Err(err))
		response.Error(c, ecode.InvalidParams)
		return
	}
	form.{{.ColumnNameCamel}} = {{.ColumnNameCamelFCL}}

	{{.TableNameCamelFCL}} := &model.{{.TableNameCamel}}{}
	err = copier.Copy({{.TableNameCamelFCL}}, form)
	if err != nil {
		response.Error(c, ecode.ErrUpdateBy{{.ColumnNameCamel}}{{.TableNameCamel}})
		return
	}
	// Note: if copier.Copy cannot assign a value to a field, add it here

	ctx := middleware.WrapCtx(c)
	err = h.iDao.UpdateBy{{.ColumnNameCamel}}(ctx, {{.TableNameCamelFCL}})
	if err != nil {
		logger.ErrorWithCtx(middleware.WrapCtx(c), "UpdateBy{{.ColumnNameCamel}} error", logger.Err(err), logger.Any("form", form))
		response.Output(c, ecode.InternalServerError.ToHTTPCode())
		return
	}

	response.Success(c)
}

// GetBy{{.ColumnNameCamel}} get a record by {{.ColumnNameCamelFCL}}
// @Summary get {{.TableNameCamelFCL}} detail
// @Description get {{.TableNameCamelFCL}} detail by {{.ColumnNameCamelFCL}}
// @Tags {{.TableNameCamelFCL}}
// @Param {{.ColumnNameCamelFCL}} path string true "{{.ColumnNameCamelFCL}}"
// @Accept json
// @Produce json
// @Success 200 {object} types.Get{{.TableNameCamel}}By{{.ColumnNameCamel}}Reply{}
// @Router /api/v1/{{.TableNameCamelFCL}}/{{{.ColumnNameCamelFCL}}} [get]
// @Security BearerAuth
func (h *{{.TableNameCamelFCL}}Handler) GetBy{{.ColumnNameCamel}}(c *gin.Context) {
	{{.ColumnNameCamelFCL}}, isAbort := get{{.TableNameCamel}}{{.ColumnNameCamel}}FromPath(c)
	if isAbort {
		response.Error(c, ecode.InvalidParams)
		return
	}

	ctx := middleware.WrapCtx(c)
	{{.TableNameCamelFCL}}, err := h.iDao.GetBy{{.ColumnNameCamel}}(ctx, {{.ColumnNameCamelFCL}})
	if err != nil {
		if errors.Is(err, database.ErrRecordNotFound) {
			logger.WarnWithCtx(middleware.WrapCtx(c), "GetBy{{.ColumnNameCamel}} not found", logger.Err(err), logger.Any("{{.ColumnNameCamelFCL}}", {{.ColumnNameCamelFCL}}))
			response.Error(c, ecode.NotFound)
		} else {
			logger.ErrorWithCtx(middleware.WrapCtx(c), "GetBy{{.ColumnNameCamel}} error", logger.Err(err), logger.Any("{{.ColumnNameCamelFCL}}", {{.ColumnNameCamelFCL}}))
			response.Output(c, ecode.InternalServerError.ToHTTPCode())
		}
		return
	}

	data := &types.{{.TableNameCamel}}ObjDetail{}
	err = copier.Copy(data, {{.TableNameCamelFCL}})
	if err != nil {
		response.Error(c, ecode.ErrGetBy{{.ColumnNameCamel}}{{.TableNameCamel}})
		return
	}
	// Note: if copier.Copy cannot assign a value to a field, add it here

	response.Success(c, gin.H{"{{.TableNameCamelFCL}}": data})
}

// List of records by query parameters
// @Summary list of {{.TableNamePluralCamelFCL}} by query parameters
// @Description list of {{.TableNamePluralCamelFCL}} by paging and conditions
// @Tags {{.TableNameCamelFCL}}
// @accept json
// @Produce json
// @Param data body types.Params true "query parameters"
// @Success 200 {object} types.List{{.TableNamePluralCamel}}Reply{}
// @Router /api/v1/{{.TableNameCamelFCL}}/list [post]
// @Security BearerAuth
func (h *{{.TableNameCamelFCL}}Handler) List(c *gin.Context) {
	form := &types.List{{.TableNamePluralCamel}}Request{}
	err := c.ShouldBindJSON(form)
	if err != nil {
		logger.WarnWithCtx(middleware.WrapCtx(c), "ShouldBindJSON error: ", logger.Err(err))
		response.Error(c, ecode.InvalidParams)
		return
	}

	ctx := middleware.WrapCtx(c)
	{{.TableNamePluralCamelFCL}}, total, err := h.iDao.GetByColumns(ctx, &form.Params)
	if err != nil {
		logger.ErrorWithCtx(middleware.WrapCtx(c), "GetByColumns error", logger.Err(err), logger.Any("form", form))
		response.Output(c, ecode.InternalServerError.ToHTTPCode())
		return
	}

	data, err := convert{{.TableNamePluralCamel}}({{.TableNamePluralCamelFCL}})
	if err != nil {
		response.Error(c, ecode.ErrList{{.TableNameCamel}})
		return
	}

	response.Success(c, gin.H{
		"{{.TableNamePluralCamelFCL}}": data,
		"total":        total,
	})
}

func get{{.TableNameCamel}}{{.ColumnNameCamel}}FromPath(c *gin.Context) ({{.GoType}}, bool) {
	{{.ColumnNameCamelFCL}}Str := c.Param("{{.ColumnNameCamelFCL}}")
{{if .IsStringType}}
	if {{.ColumnNameCamelFCL}}Str == "" {
		logger.WarnWithCtx(middleware.WrapCtx(c), "{{.ColumnNameCamelFCL}} is empty")
		return "", true
	}
	return {{.ColumnNameCamelFCL}}Str, false
{{else}}
	{{.ColumnNameCamelFCL}}, err := utils.StrTo{{.GoTypeFCU}}E({{.ColumnNameCamelFCL}}Str)
	if err != nil || {{.ColumnNameCamelFCL}}Str == "" {
		logger.WarnWithCtx(middleware.WrapCtx(c), "StrTo{{.GoTypeFCU}}E error: ", logger.String("{{.ColumnNameCamelFCL}}Str", {{.ColumnNameCamelFCL}}Str))
		return 0, true
	}
	return {{.ColumnNameCamelFCL}}, false
{{end}}
}

func convert{{.TableNameCamel}}({{.TableNameCamelFCL}} *model.{{.TableNameCamel}}) (*types.{{.TableNameCamel}}ObjDetail, error) {
	data := &types.{{.TableNameCamel}}ObjDetail{}
	err := copier.Copy(data, {{.TableNameCamelFCL}})
	if err != nil {
		return nil, err
	}
	// Note: if copier.Copy cannot assign a value to a field, add it here

	return data, nil
}

func convert{{.TableNamePluralCamel}}(fromValues []*model.{{.TableNameCamel}}) ([]*types.{{.TableNameCamel}}ObjDetail, error) {
	toValues := []*types.{{.TableNameCamel}}ObjDetail{}
	for _, v := range fromValues {
		data, err := convert{{.TableNameCamel}}(v)
		if err != nil {
			return nil, err
		}
		toValues = append(toValues, data)
	}

	return toValues, nil
}
