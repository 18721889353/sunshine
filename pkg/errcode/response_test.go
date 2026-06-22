package errcode

import (
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/18721889353/sunshine/pkg/utils"
)

func runHTTPServer(isFromRPC bool) string {
	serverAddr, requestAddr := utils.GetLocalHTTPAddrPairs()

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	httpErrors := []*Error{Forbidden, TooManyRequests, MethodNotAllowed}
	rpcStatus := []*RPCStatus{StatusDeadlineExceeded, StatusPermissionDenied, StatusAlreadyExists}
	resp := NewResponser(isFromRPC, isFromRPC, httpErrors, rpcStatus)

	r.GET("/ping", func(c *gin.Context) {
		resp.Success(c, "ping")
	})
	r.GET("/unknown", func(c *gin.Context) {
		resp.Error(c, errors.New("unknown error"))
	})

	if isFromRPC {
		r.GET("/grpc/params", func(c *gin.Context) {
			resp.Error(c, StatusInvalidParams.Err("email is required"))
		})
		r.GET("/grpc/src_params", func(c *gin.Context) {
			resp.Error(c, StatusInvalidParams.ToRPCErr("name is required"))
		})
		r.GET("/grpc/mark_params", func(c *gin.Context) {
			resp.Error(c, StatusInvalidParams.ErrToHTTP("id must be a number"))
		})

		r.GET("/grpc/internal", func(c *gin.Context) {
			resp.Error(c, StatusInternalServerError.Err())
		})
		r.GET("/grpc/src_internal", func(c *gin.Context) {
			resp.Error(c, StatusInternalServerError.ToRPCErr())
		})
		r.GET("/grpc/mark_internal", func(c *gin.Context) {
			resp.Error(c, StatusInternalServerError.ErrToHTTP())
		})

		r.GET("/grpc/unavailable", func(c *gin.Context) {
			resp.Error(c, StatusServiceUnavailable.Err())
		})
		r.GET("/grpc/src_unavailable", func(c *gin.Context) {
			resp.Error(c, StatusServiceUnavailable.ToRPCErr())
		})
		r.GET("/grpc/mark_unavailable", func(c *gin.Context) {
			resp.Error(c, StatusServiceUnavailable.ErrToHTTP())
		})

		r.GET("/grpc/notfound", func(c *gin.Context) {
			resp.Error(c, StatusNotFound.Err())
		})
		r.GET("/grpc/src_notfound", func(c *gin.Context) {
			resp.Error(c, StatusNotFound.ToRPCErr())
		})
		r.GET("/grpc/mark_notfound", func(c *gin.Context) {
			resp.Error(c, StatusNotFound.ErrToHTTP())
		})

		r.GET("/grpc/permission", func(c *gin.Context) {
			resp.Error(c, StatusPermissionDenied.Err())
		})
		r.GET("/grpc/src_permission", func(c *gin.Context) {
			resp.Error(c, StatusPermissionDenied.ToRPCErr())
		})
		r.GET("/grpc/mark_permission", func(c *gin.Context) {
			resp.Error(c, StatusPermissionDenied.ErrToHTTP())
		})

		r.GET("/grpc/conflict", func(c *gin.Context) {
			resp.Error(c, StatusConflict.Err())
		})
		r.GET("/grpc/src_conflict", func(c *gin.Context) {
			resp.Error(c, StatusConflict.ToRPCErr())
		})
		r.GET("/grpc/mark_conflict", func(c *gin.Context) {
			resp.Error(c, StatusConflict.ErrToHTTP())
		})
	} else {
		r.GET("/http/params", func(c *gin.Context) {
			resp.Error(c, InvalidParams.Err("name is required"))
		})
		r.GET("/http/mark_params", func(c *gin.Context) {
			resp.Error(c, InvalidParams.ErrToHTTP("id must be a number"))
		})

		r.GET("/http/internal", func(c *gin.Context) {
			resp.Error(c, InternalServerError.Err())
		})
		r.GET("/http/mark_internal", func(c *gin.Context) {
			resp.Error(c, InternalServerError.ErrToHTTP())
		})

		r.GET("/http/notfound", func(c *gin.Context) {
			resp.Error(c, NotFound.Err())
		})
		r.GET("/http/mark_notfound", func(c *gin.Context) {
			resp.Error(c, NotFound.ErrToHTTP())
		})

		r.GET("/http/forbidden", func(c *gin.Context) {
			resp.Error(c, Forbidden.Err())
		})
		r.GET("/http/mark_forbidden", func(c *gin.Context) {
			resp.Error(c, Forbidden.ErrToHTTP())
		})

		r.GET("/http/too_many", func(c *gin.Context) {
			resp.Error(c, TooManyRequests.Err())
		})
		r.GET("/http/mark_too_many", func(c *gin.Context) {
			resp.Error(c, TooManyRequests.ErrToHTTP())
		})

		r.GET("/http/conflict", func(c *gin.Context) {
			resp.Error(c, Conflict.Err())
		})
		r.GET("/http/mark_conflict", func(c *gin.Context) {
			resp.Error(c, Conflict.ErrToHTTP())
		})
	}

	go func() {
		err := r.Run(serverAddr)
		if err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Millisecond * 200)

	return requestAddr
}

func TestRPCResponse(t *testing.T) {
	requestAddr := runHTTPServer(true)

	result, err := http.Get(requestAddr + "/ping")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusOK)

	result, err = http.Get(requestAddr + "/unknown")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusOK)

	result, err = http.Get(requestAddr + "/grpc/params")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusOK)
	data, e := io.ReadAll(result.Body)
	t.Log(string(data), e)
	result, err = http.Get(requestAddr + "/grpc/src_params")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusOK)
	data, e = io.ReadAll(result.Body)
	t.Log(string(data), e)
	result, err = http.Get(requestAddr + "/grpc/mark_params")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusBadRequest)
	data, e = io.ReadAll(result.Body)
	t.Log(string(data), e)

	result, err = http.Get(requestAddr + "/grpc/internal")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusInternalServerError)
	result, err = http.Get(requestAddr + "/grpc/src_internal")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusInternalServerError)
	result, err = http.Get(requestAddr + "/grpc/mark_internal")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusInternalServerError)

	result, err = http.Get(requestAddr + "/grpc/unavailable")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusServiceUnavailable)
	result, err = http.Get(requestAddr + "/grpc/src_unavailable")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusServiceUnavailable)
	result, err = http.Get(requestAddr + "/grpc/mark_unavailable")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusServiceUnavailable)

	result, err = http.Get(requestAddr + "/grpc/notfound")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusOK)
	result, err = http.Get(requestAddr + "/grpc/src_notfound")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusOK)
	result, err = http.Get(requestAddr + "/grpc/mark_notfound")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusNotFound)

	result, err = http.Get(requestAddr + "/grpc/permission")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusUnauthorized)
	result, err = http.Get(requestAddr + "/grpc/src_permission")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusUnauthorized)
	result, err = http.Get(requestAddr + "/grpc/mark_permission")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusUnauthorized)

	result, err = http.Get(requestAddr + "/grpc/conflict")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusOK)
	result, err = http.Get(requestAddr + "/grpc/src_conflict")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusConflict)
	result, err = http.Get(requestAddr + "/grpc/mark_conflict")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusConflict)
}

func TestHTTPResponse(t *testing.T) {
	requestAddr := runHTTPServer(false)

	result, err := http.Get(requestAddr + "/ping")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusOK)

	result, err = http.Get(requestAddr + "/unknown")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusOK)

	result, err = http.Get(requestAddr + "/http/params")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusOK)
	data, e := io.ReadAll(result.Body)
	t.Log(string(data), e)
	result, err = http.Get(requestAddr + "/http/mark_params")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusBadRequest)
	data, e = io.ReadAll(result.Body)
	t.Log(string(data), e)

	result, err = http.Get(requestAddr + "/http/internal")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusInternalServerError)
	result, err = http.Get(requestAddr + "/http/mark_internal")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusInternalServerError)

	result, err = http.Get(requestAddr + "/http/notfound")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusOK)
	result, err = http.Get(requestAddr + "/http/mark_notfound")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusNotFound)

	result, err = http.Get(requestAddr + "/http/forbidden")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusForbidden)
	result, err = http.Get(requestAddr + "/http/mark_forbidden")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusForbidden)

	result, err = http.Get(requestAddr + "/http/too_many")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusTooManyRequests)
	result, err = http.Get(requestAddr + "/http/mark_too_many")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusTooManyRequests)

	result, err = http.Get(requestAddr + "/http/conflict")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusOK)
	result, err = http.Get(requestAddr + "/http/mark_conflict")
	assert.NoError(t, err)
	assert.Equal(t, result.StatusCode, http.StatusConflict)
}

func TestParseCodeAndMsgError(t *testing.T) {
	errStr := "rpc error: code = Unknown desc = rpc error: code = Unknown desc = code = 204011, msg = wrong account or password"
	err := errors.New(errStr)
	st, _ := status.FromError(err)
	if st.Code() == codes.Unknown {
		code, msg := parseCodeAndMsg(st.String())
		t.Log("regexp: ", code, msg)
	}
	code, msg := parseCodeAndMsg2(st.String())
	t.Log("strings: ", code, msg)

	st, _ = status.FromError(SkipResponse)
	t.Log(st.Code(), st.Message())
}

func TestNilDataAsEmptySlice(t *testing.T) {
	serverAddr, requestAddr := utils.GetLocalHTTPAddrPairs()

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// data 为 nil 时默认返回 [] 而非 null
	resp := NewResponser(false, false, nil, nil)

	// 测试 data = nil
	r.GET("/success_nil", func(c *gin.Context) {
		resp.Success(c, nil)
	})
	// 测试 data = 非 nil 字符串
	r.GET("/success_data", func(c *gin.Context) {
		resp.Success(c, "hello")
	})
	// 测试 Success2 且 data = nil
	r.GET("/success2_nil", func(c *gin.Context) {
		resp.Success2(c, "0", "ok", nil)
	})
	// 测试错误响应（默认 data 传 nil）
	r.GET("/error_params", func(c *gin.Context) {
		resp.Error(c, InvalidParams.Err("test"))
	})

	go func() {
		err := r.Run(serverAddr)
		if err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Millisecond * 200)

	// 验证 data = nil 时返回 []
	result, err := http.Get(requestAddr + "/success_nil")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, result.StatusCode)
	body, _ := io.ReadAll(result.Body)
	t.Log("success_nil:", string(body))
	assert.Contains(t, string(body), `"data":[]`)
	result.Body.Close()

	// 验证正常数据不受影响
	result, err = http.Get(requestAddr + "/success_data")
	assert.NoError(t, err)
	body, _ = io.ReadAll(result.Body)
	t.Log("success_data:", string(body))
	assert.Contains(t, string(body), `"data":"hello"`)
	result.Body.Close()

	// 验证 Success2 且 data = nil 时返回 []
	result, err = http.Get(requestAddr + "/success2_nil")
	assert.NoError(t, err)
	body, _ = io.ReadAll(result.Body)
	t.Log("success2_nil:", string(body))
	assert.Contains(t, string(body), `"data":[]`)
	result.Body.Close()

	// 验证错误响应也返回 []
	result, err = http.Get(requestAddr + "/error_params")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, result.StatusCode)
	body, _ = io.ReadAll(result.Body)
	t.Log("error_params:", string(body))
	assert.Contains(t, string(body), `"data":[]`)
	result.Body.Close()
}

func TestSuccessUnwrap(t *testing.T) {
	serverAddr, requestAddr := utils.GetLocalHTTPAddrPairs()

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	resp := NewResponser(false, false, nil, nil)

	// 定义测试用的结构体：仅有一个切片字段，应触发自动拆包
	type Item struct {
		Name string `json:"name"`
	}
	type Wrapper struct {
		Items []Item `json:"items"`
	}

	// 多个切片字段的结构体，不触发拆包
	type MultiSlice struct {
		Items  []Item `json:"items"`
		Others []int  `json:"others"`
	}

	// 指针类型结构体
	type PtrWrapper struct {
		List []string `json:"list"`
	}

	r.GET("/unwrap_match", func(c *gin.Context) {
		resp.Success(c, Wrapper{Items: []Item{{Name: "a"}, {Name: "b"}}})
	})
	r.GET("/unwrap_nil_ptr", func(c *gin.Context) {
		resp.Success(c, (*Wrapper)(nil))
	})
	r.GET("/unwrap_multi", func(c *gin.Context) {
		resp.Success(c, MultiSlice{Items: []Item{{Name: "a"}}, Others: []int{1}})
	})
	r.GET("/unwrap_ptr", func(c *gin.Context) {
		resp.Success(c, &PtrWrapper{List: []string{"x", "y"}})
	})
	r.GET("/unwrap_non_struct", func(c *gin.Context) {
		resp.Success(c, "plain string")
	})

	go func() {
		err := r.Run(serverAddr)
		if err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Millisecond * 200)

	// 单切片字段结构体应自动拆包，返回 items 数组
	result, err := http.Get(requestAddr + "/unwrap_match")
	assert.NoError(t, err)
	body, _ := io.ReadAll(result.Body)
	t.Log("unwrap_match:", string(body))
	assert.Contains(t, string(body), `"data":[{"name":"a"},{"name":"b"}]`)
	result.Body.Close()

	// nil 指针或 nil data 统一返回 []
	result, err = http.Get(requestAddr + "/unwrap_nil_ptr")
	assert.NoError(t, err)
	body, _ = io.ReadAll(result.Body)
	t.Log("unwrap_nil_ptr:", string(body))
	assert.Contains(t, string(body), `"data":[]`)
	result.Body.Close()

	// 多切片字段不拆包，返回完整结构体
	result, err = http.Get(requestAddr + "/unwrap_multi")
	assert.NoError(t, err)
	body, _ = io.ReadAll(result.Body)
	t.Log("unwrap_multi:", string(body))
	assert.Contains(t, string(body), `"items"`)
	assert.Contains(t, string(body), `"others"`)
	result.Body.Close()

	// 指针类型也应能正确拆包
	result, err = http.Get(requestAddr + "/unwrap_ptr")
	assert.NoError(t, err)
	body, _ = io.ReadAll(result.Body)
	t.Log("unwrap_ptr:", string(body))
	assert.Contains(t, string(body), `"data":["x","y"]`)
	result.Body.Close()

	// 非结构体类型直接返回原值
	result, err = http.Get(requestAddr + "/unwrap_non_struct")
	assert.NoError(t, err)
	body, _ = io.ReadAll(result.Body)
	t.Log("unwrap_non_struct:", string(body))
	assert.Contains(t, string(body), `"data":"plain string"`)
	result.Body.Close()
}

var mcReg = regexp.MustCompile(`code\s*=\s*(\d+),\s*msg\s*=\s*(.+)`)

func parseCodeAndMsg2(errStr string) (int, string) {
	matches := mcReg.FindStringSubmatch(errStr)
	if len(matches) == 3 {
		code, _ := strconv.Atoi(matches[1])
		msg := matches[2]
		return code, msg
	}
	return 0, errStr
}

func BenchmarkName(b *testing.B) {
	errStr := "rpc error: code = Unknown desc = rpc error: code = Unknown desc = code = 204011, msg = wrong account or password"
	err := errors.New(errStr)
	st, _ := status.FromError(err)
	if st.Code() == codes.Unknown {
		for i := 0; i < b.N; i++ {
			parseCodeAndMsg(st.String())
			//parseCodeAndMsg2(st.String())
		}
	}
}
