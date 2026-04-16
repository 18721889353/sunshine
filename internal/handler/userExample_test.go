package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jinzhu/copier"
	"github.com/stretchr/testify/assert"

	"github.com/18721889353/sunshine/pkg/gotest"
	"github.com/18721889353/sunshine/pkg/httpcli"
	"github.com/18721889353/sunshine/pkg/sgorm/query"
	"github.com/18721889353/sunshine/pkg/utils"

	"github.com/18721889353/sunshine/internal/cache"
	"github.com/18721889353/sunshine/internal/dao"
	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/internal/model"
	"github.com/18721889353/sunshine/internal/types"
)

func newUserExampleHandler() *gotest.Handler {
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
	h.IHandler = &userExampleHandler{iDao: d.IDao.(dao.UserExampleDao)}
	iHandler := h.IHandler.(UserExampleHandler)

	testFns := []gotest.RouterInfo{
		{
			FuncName:    "Create",
			Method:      http.MethodPost,
			Path:        "/userExample",
			HandlerFunc: iHandler.Create,
		},
		{
			FuncName:    "DeleteByID",
			Method:      http.MethodDelete,
			Path:        "/userExample/:id",
			HandlerFunc: iHandler.DeleteByID,
		},
		{
			FuncName:    "UpdateByID",
			Method:      http.MethodPut,
			Path:        "/userExample/:id",
			HandlerFunc: iHandler.UpdateByID,
		},
		{
			FuncName:    "GetByID",
			Method:      http.MethodGet,
			Path:        "/userExample/:id",
			HandlerFunc: iHandler.GetByID,
		},
		{
			FuncName:    "List",
			Method:      http.MethodPost,
			Path:        "/userExample/list",
			HandlerFunc: iHandler.List,
		},
	}

	h.GoRunHTTPServer(testFns)

	time.Sleep(time.Millisecond * 200)
	return h
}

func Test_userExampleHandler_Create(t *testing.T) {
	h := newUserExampleHandler()
	defer h.Close()
	testData := &types.CreateUserExampleRequest{}
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
	testData = &types.CreateUserExampleRequest{
		Name:     "foo",
		Password: "f447b20a7fcbf53a5d5be013ea0b15af",
		Email:    "foo@bar.com",
		Phone:    "+8616000000001",
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
	assert.Error(t, err)
	// delete the templates code end
}

func Test_userExampleHandler_DeleteByID(t *testing.T) {
	h := newUserExampleHandler()
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
	assert.Error(t, err)
}

func Test_userExampleHandler_UpdateByID(t *testing.T) {
	h := newUserExampleHandler()
	defer h.Close()
	testData := &types.UpdateUserExampleByIDRequest{}
	_ = copier.Copy(testData, h.TestData.(*model.UserExample))

	h.MockDao.SQLMock.ExpectBegin()
	h.MockDao.SQLMock.ExpectExec("UPDATE .*").
		WithArgs(h.MockDao.AnyTime, testData.ID). // adjusted for the amount of test data
		WillReturnResult(sqlmock.NewResult(int64(testData.ID), 1))
	h.MockDao.SQLMock.ExpectCommit()

	client := httpcli.New()
	result := make(map[string]interface{})
	resp, err := client.Request(context.Background()).
		SetBody(testData).
		SetResult(&result).
		Put(h.GetRequestURL("UpdateByID", testData.ID))
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
	assert.Error(t, err)
}

func Test_userExampleHandler_GetByID(t *testing.T) {
	h := newUserExampleHandler()
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
	assert.Error(t, err)
}

func Test_userExampleHandler_List(t *testing.T) {
	h := newUserExampleHandler()
	defer h.Close()
	testData := h.TestData.(*model.UserExample)

	// column names and corresponding data
	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(testData.ID)

	h.MockDao.SQLMock.ExpectQuery("SELECT .*").WillReturnRows(rows)

	client := httpcli.New()
	result := make(map[string]interface{})
	resp, err := client.Request(context.Background()).
		SetBody(&types.ListUserExamplesRequest{query.Params{
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
		SetResult(&result).
		Post(h.GetRequestURL("List"))
	assert.NoError(t, err)

	// get error test
	_, err = client.Request(context.Background()).
		SetBody(&types.ListUserExamplesRequest{query.Params{
			Page:  0,
			Limit: 10,
			Sort:  "unknown-column",
		}}).
		SetResult(&result).
		Post(h.GetRequestURL("List"))
	assert.Error(t, err)
}

func TestNewUserExampleHandler(t *testing.T) {
	defer func() {
		recover()
	}()
	_ = NewUserExampleHandler()
}
