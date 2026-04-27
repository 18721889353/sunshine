package routers

import (
	"net/http"
	"reflect"
	"strconv"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/status"
)

type customResponse struct{}

// NewCustomResponse 创建自定义响应处理器
func NewCustomResponse() *customResponse {
	return &customResponse{}
}
func (r *customResponse) response(c *gin.Context, code int, customCode, msg string, data interface{}) {
	c.JSON(code, gin.H{
		"code": customCode,
		"msg":  msg,
		"data": data,
	})
}

// extractCodeFromStruct 从结构体中提取CustomCode字段
func extractCodeFromStruct(v reflect.Value) (string, bool) {
	codeField := v.FieldByName("CustomCode")
	if !codeField.IsValid() || !codeField.CanInterface() {
		return "", false
	}

	switch codeField.Kind() {
	case reflect.String:
		if code := codeField.String(); code != "" {
			return code, true
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(codeField.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(codeField.Uint(), 10), true
	}
	return "", false
}

// structToMapWithoutCode 将结构体转换为map,排除CustomCode字段
func structToMapWithoutCode(v reflect.Value) map[string]interface{} {
	resultMap := make(map[string]interface{})
	structType := v.Type()

	for i := 0; i < v.NumField(); i++ {
		fieldType := structType.Field(i)
		fieldValue := v.Field(i)

		if fieldType.IsExported() && fieldType.Name != "CustomCode" {
			jsonKey := string(fieldType.Name[0]+32) + fieldType.Name[1:]
			if fieldType.Name[0] >= 'A' && fieldType.Name[0] <= 'Z' {
				resultMap[jsonKey] = fieldValue.Interface()
			}
		}
	}
	return resultMap
}

// processMapData 处理map类型的数据,提取code字段
func processMapData(v reflect.Value) (interface{}, string, bool) {
	if v.Type().Key().Kind() != reflect.String {
		return nil, "", false
	}

	newMap := reflect.MakeMap(v.Type())
	customCode := ""
	iter := v.MapRange()

	for iter.Next() {
		key := iter.Key()
		value := iter.Value()

		if key.String() != "code" && key.String() != "CustomCode" {
			newMap.SetMapIndex(key, value)
		} else {
			if value.Kind() == reflect.String {
				if code := value.String(); code != "" {
					customCode = code
				}
			} else {
				customCode = strconv.FormatInt(value.Int(), 10)
			}
		}
	}
	return newMap.Interface(), customCode, true
}

func (r *customResponse) Success(c *gin.Context, data interface{}) {
	customCode := "0000"
	processedData := data

	if data == nil {
		r.response(c, http.StatusOK, customCode, "发放成功", processedData)
		return
	}

	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() == reflect.Struct {
		if code, ok := extractCodeFromStruct(v); ok {
			customCode = code
		}
		processedData = structToMapWithoutCode(v)
	} else if v.Kind() == reflect.Map {
		if newData, code, ok := processMapData(v); ok {
			processedData = newData
			if code != "" {
				customCode = code
			}
		}
	}

	msg := "发放成功"
	if customCode != "0000" {
		msg = "进行中"
	}
	r.response(c, http.StatusOK, customCode, msg, processedData)
}
func (r *customResponse) Success2(_ *gin.Context, _, _ string, _ interface{}) {
}

func (r *customResponse) ParamError(c *gin.Context, _ error) {
	r.response(c, http.StatusOK, "1002", http.StatusText(http.StatusBadRequest), struct{}{})
}

func (r *customResponse) Error(c *gin.Context, err error) bool {
	//r.response(c, http.StatusOK, "1001", "处理中", struct{}{})
	////e := errcode.ParseError(err)
	st, _ := status.FromError(err)
	r.response(c, http.StatusOK, strconv.Itoa(int(st.Code())), st.Message(), struct{}{})
	return false
}
