package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/18721889353/sunshine/api/types"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/copier"
	"github.com/stretchr/testify/assert"

	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/18721889353/sunshine/pkg/gotest"
	"github.com/18721889353/sunshine/pkg/httpcli"
	"github.com/18721889353/sunshine/pkg/utils"

	serverNameExampleV1 "github.com/18721889353/sunshine/api/serverNameExample/v1"
	"github.com/18721889353/sunshine/internal/cache"
	"github.com/18721889353/sunshine/internal/dao"
	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/internal/ecode"
	"github.com/18721889353/sunshine/internal/model"
)

func newUserExamplePbHandler() *gotest.Handler {
	testData := &model.UserExample{}
	testData.ID = 1
	// you can set the other fields of testData here, such as:
	//testData.CreatedAt = time.Now()
	//testData.UpdatedAt = testData.CreatedAt

	// init mock cache
	c := gotest.NewCache(map[string]interface{}{utils.Uint64ToStr(testData.ID): testData})
	c.ICache = cache.NewUserExampleCache(&database.CacheType{
		CType: "redis",
		Rdb:   c.RedisClient,
	})

	// init mock dao
	d := gotest.NewDao(c, testData)
	d.IDao = dao.NewUserExampleDao(d.DB, c.ICache.(cache.UserExampleCache))

	// init mock handler
	h := gotest.NewHandler(d, testData)
	h.IHandler = &userExamplePbHandler{userExampleDao: d.IDao.(dao.UserExampleDao)}
	iHandler := h.IHandler.(serverNameExampleV1.UserExampleLogicer)

	testFns := []gotest.RouterInfo{
		{
			FuncName: "Create",
			Method:   http.MethodPost,
			Path:     "/userExample",
			HandlerFunc: func(c *gin.Context) {
				req := &serverNameExampleV1.CreateUserExampleRequest{}
				_ = c.ShouldBindJSON(req)
				_, err := iHandler.Create(c, req)
				if err != nil {
					response.Error(c, ecode.ErrCreateUserExample)
					return
				}
				response.Success(c)
			},
		},
		{
			FuncName: "DeleteByID",
			Method:   http.MethodDelete,
			Path:     "/userExample/:id",
			HandlerFunc: func(c *gin.Context) {
				req := &serverNameExampleV1.DeleteUserExampleByIDRequest{
					Id: utils.StrToUint64(c.Param("id")),
				}
				_, err := iHandler.DeleteByID(c, req)
				if err != nil {
					response.Error(c, ecode.ErrDeleteByIDUserExample)
					return
				}
				response.Success(c)
			},
		},
		{
			FuncName: "UpdateByID",
			Method:   http.MethodPut,
			Path:     "/userExample/:id",
			HandlerFunc: func(c *gin.Context) {
				req := &serverNameExampleV1.UpdateUserExampleByIDRequest{}
				_ = c.ShouldBindJSON(req)
				req.Id = utils.StrToUint64(c.Param("id"))
				_, err := iHandler.UpdateByID(c, req)
				if err != nil {
					response.Error(c, ecode.ErrUpdateByIDUserExample)
					return
				}
				response.Success(c)
			},
		},
		{
			FuncName: "GetByID",
			Method:   http.MethodGet,
			Path:     "/userExample/:id",
			HandlerFunc: func(c *gin.Context) {
				req := &serverNameExampleV1.GetUserExampleByIDRequest{
					Id: utils.StrToUint64(c.Param("id")),
				}
				_, err := iHandler.GetByID(c, req)
				if err != nil {
					response.Error(c, ecode.ErrGetByIDUserExample)
					return
				}
				response.Success(c)
			},
		},
		{
			FuncName: "List",
			Method:   http.MethodPost,
			Path:     "/userExample/list",
			HandlerFunc: func(c *gin.Context) {
				req := &serverNameExampleV1.ListUserExampleRequest{}
				_ = c.ShouldBindJSON(req)
				_, err := iHandler.List(c, req)
				if err != nil {
					response.Error(c, ecode.ErrListUserExample)
					return
				}
				response.Success(c)
			},
		},
	}

	h.GoRunHTTPServer(testFns)

	time.Sleep(time.Millisecond * 200)
	return h
}

func Test_userExamplePbHandler_Create(t *testing.T) {
	h := newUserExamplePbHandler()
	defer h.Close()
	testData := &serverNameExampleV1.CreateUserExampleRequest{}
	_ = copier.Copy(testData, h.TestData.(*model.UserExample))

	h.MockDao.SQLMock.ExpectBegin()
	args := h.MockDao.GetAnyArgs(h.TestData)
	h.MockDao.SQLMock.ExpectExec("INSERT INTO .*").
		WithArgs(args[:len(args)-1]...). // adjusted for the amount of test data
		WillReturnResult(sqlmock.NewResult(1, 1))
	h.MockDao.SQLMock.ExpectCommit()

	client := httpcli.New()
	result := make(map[string]interface{})
	resp, err := client.Request(context.Background()).
		SetBody(testData).
		SetResult(&result).
		Post(h.GetRequestURL("Create"))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.IsSuccess() {
		t.Fatalf("request failed: %s", resp.String())
	}

	t.Logf("%+v", result)
	// delete the templates code start
	testData = &serverNameExampleV1.CreateUserExampleRequest{
		Name:     "foo",
		Password: "f447b20a7fcbf53a5d5be013ea0b15af",
		Email:    "foo@bar.com",
		Phone:    "16000000001",
		Avatar:   "http://foo/1.jpg",
		Age:      10,
		Gender:   1,
	}
	result = make(map[string]interface{})
	resp, err = client.Request(context.Background()).
		SetBody(testData).
		SetResult(&result).
		Post(h.GetRequestURL("Create"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", result)

	h.MockDao.SQLMock.ExpectBegin()
	h.MockDao.SQLMock.ExpectCommit()
	// create error test
	result = make(map[string]interface{})
	_, err = client.Request(context.Background()).
		SetBody(testData).
		SetResult(&result).
		Post(h.GetRequestURL("Create"))
	assert.NoError(t, err)
	// delete the templates code end
}

func Test_userExamplePbHandler_DeleteByID(t *testing.T) {
	h := newUserExamplePbHandler()
	defer h.Close()
	testData := h.TestData.(*model.UserExample)
	expectedSQLForDeletion := "UPDATE .*"
	expectedArgsForDeletionTime := h.MockDao.AnyTime

	h.MockDao.SQLMock.ExpectBegin()
	h.MockDao.SQLMock.ExpectExec(expectedSQLForDeletion).
		WithArgs(expectedArgsForDeletionTime, testData.ID). // adjusted for the amount of test data
		WillReturnResult(sqlmock.NewResult(int64(testData.ID), 1))
	h.MockDao.SQLMock.ExpectCommit()

	client := httpcli.New()
	result := make(map[string]interface{})
	resp, err := client.Request(context.Background()).
		SetResult(&result).
		Delete(h.GetRequestURL("DeleteByID", testData.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.IsSuccess() {
		t.Fatalf("request failed: %+v", result)
	}

	// zero id error test
	_, err = client.Request(context.Background()).
		SetResult(&result).
		Delete(h.GetRequestURL("DeleteByID", 0))
	assert.NoError(t, err)

	// delete error test
	_, err = client.Request(context.Background()).
		SetResult(&result).
		Delete(h.GetRequestURL("DeleteByID", 111))
	assert.NoError(t, err)
}

func Test_userExamplePbHandler_UpdateByID(t *testing.T) {
	h := newUserExamplePbHandler()
	defer h.Close()
	testData := &serverNameExampleV1.UpdateUserExampleByIDRequest{}
	_ = copier.Copy(testData, h.TestData.(*model.UserExample))
	testData.Id = h.TestData.(*model.UserExample).ID

	h.MockDao.SQLMock.ExpectBegin()
	h.MockDao.SQLMock.ExpectExec("UPDATE .*").
		WithArgs(h.MockDao.AnyTime, testData.Id). // adjusted for the amount of test data
		WillReturnResult(sqlmock.NewResult(int64(testData.Id), 1))
	h.MockDao.SQLMock.ExpectCommit()

	client := httpcli.New()
	result := make(map[string]interface{})
	resp, err := client.Request(context.Background()).
		SetBody(testData).
		SetResult(&result).
		Put(h.GetRequestURL("UpdateByID", testData.Id))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.IsSuccess() {
		t.Fatalf("request failed: %+v", result)
	}

	// zero id error test
	_, err = client.Request(context.Background()).
		SetBody(testData).
		SetResult(&result).
		Put(h.GetRequestURL("UpdateByID", 0))
	assert.NoError(t, err)

	// update error test
	_, err = client.Request(context.Background()).
		SetBody(testData).
		SetResult(&result).
		Put(h.GetRequestURL("UpdateByID", 111))
	assert.NoError(t, err)
}

func Test_userExamplePbHandler_GetByID(t *testing.T) {
	h := newUserExamplePbHandler()
	defer h.Close()
	testData := h.TestData.(*model.UserExample)

	// column names and corresponding data
	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(testData.ID)

	h.MockDao.SQLMock.ExpectQuery("SELECT .*").
		WithArgs(testData.ID).
		WillReturnRows(rows)

	client := httpcli.New()
	result := make(map[string]interface{})
	resp, err := client.Request(context.Background()).
		SetResult(&result).
		Get(h.GetRequestURL("GetByID", testData.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.IsSuccess() {
		t.Fatalf("request failed: %+v", result)
	}

	// zero id error test
	_, err = client.Request(context.Background()).
		SetResult(&result).
		Get(h.GetRequestURL("GetByID", 0))
	assert.NoError(t, err)

	// get error test
	_, err = client.Request(context.Background()).
		SetResult(&result).
		Get(h.GetRequestURL("GetByID", 111))
	assert.NoError(t, err)
}

func Test_userExamplePbHandler_List(t *testing.T) {
	h := newUserExamplePbHandler()
	defer h.Close()
	testData := h.TestData.(*model.UserExample)

	// column names and corresponding data
	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(testData.ID)

	h.MockDao.SQLMock.ExpectQuery("SELECT .*").WillReturnRows(rows)

	client := httpcli.New()
	result := make(map[string]interface{})
	resp, err := client.Request(context.Background()).
		SetBody(&serverNameExampleV1.ListUserExampleRequest{
			Params: &types.Params{
				Page:  0,
				Limit: 10,
				Sort:  "ignore count", // ignore test count
			}}).
		SetResult(&result).
		Post(h.GetRequestURL("List"))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.IsSuccess() {
		t.Fatalf("request failed: %+v", result)
	}

	// nil params error test
	_, err = client.Request(context.Background()).
		SetBody(&serverNameExampleV1.ListUserExampleRequest{}).
		SetResult(&result).
		Post(h.GetRequestURL("List"))
	assert.NoError(t, err)

	// get error test
	_, err = client.Request(context.Background()).
		SetBody(&serverNameExampleV1.ListUserExampleRequest{Params: &types.Params{
			Page:  0,
			Limit: 10,
		}}).
		SetResult(&result).
		Post(h.GetRequestURL("List"))
	assert.NoError(t, err)
}

func TestNewUserExamplePbHandler(t *testing.T) {
	defer func() {
		recover()
	}()
	_ = NewUserExamplePbHandler()
}
