package routers

import (
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/status"
	"net/http"
	"reflect"
	"strconv"
)

type customResponse struct{}

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

func (r *customResponse) Success(c *gin.Context, data interface{}) {
	var customCode = "0000"
	// 创建一个新的数据副本，用于可能的修改
	processedData := data

	if data != nil {
		v := reflect.ValueOf(data)
		// 处理指针类型
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}

		// 如果是结构体类型
		if v.Kind() == reflect.Struct {
			// 查找code字段
			codeField := v.FieldByName("CustomCode")
			if codeField.IsValid() && codeField.CanInterface() {
				// 提取code值
				switch codeField.Kind() {
				case reflect.String:
					extractedCode := codeField.String()
					// 只有当提取的code不为空时才使用它
					if extractedCode != "" {
						customCode = extractedCode
					}
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
					customCode = strconv.FormatInt(codeField.Int(), 10)
				case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
					customCode = strconv.FormatUint(codeField.Uint(), 10)
				}

				// 使用 map[string]interface{} 来构建不包含 code 字段的数据
				resultMap := make(map[string]interface{})
				structType := v.Type()

				// 遍历所有字段
				for i := 0; i < v.NumField(); i++ {
					fieldType := structType.Field(i)
					fieldValue := v.Field(i)

					// 只处理可导出的字段（大写字母开头）
					if fieldType.IsExported() && fieldType.Name != "CustomCode" {
						// 将字段名转换为小写形式作为 JSON 键名
						// 注意：这里简化处理，实际项目中可能需要考虑 struct tag
						jsonKey := string(fieldType.Name[0]+32) + fieldType.Name[1:] // 转换为小写首字母
						if fieldType.Name[0] >= 'A' && fieldType.Name[0] <= 'Z' {
							resultMap[jsonKey] = fieldValue.Interface()
						}
					}
				}

				processedData = resultMap
			}
		} else if v.Kind() == reflect.Map {
			// 如果是map类型
			if v.Type().Key().Kind() == reflect.String {
				// 创建一个新的map副本
				newMap := reflect.MakeMap(v.Type())
				iter := v.MapRange()

				// 遍历map中的所有键值对
				for iter.Next() {
					key := iter.Key()
					value := iter.Value()

					// 如果键不是"code"，则复制到新map
					if key.String() != "code" && key.String() != "CustomCode" {
						newMap.SetMapIndex(key, value)
					} else {
						// 如果键是"code"，则提取值作为customCode
						if value.Kind() == reflect.String {
							extractedCode := value.String()
							// 只有当提取的code不为空时才使用它
							if extractedCode != "" {
								customCode = extractedCode
							}
						} else {
							customCode = strconv.FormatInt(value.Int(), 10)
						}
					}
				}

				processedData = newMap.Interface()
			}
		}
	}
	msg := "发放成功"
	if customCode != "0000" {
		msg = "进行中"
	}
	r.response(c, http.StatusOK, customCode, msg, processedData)
}
func (r *customResponse) Success2(c *gin.Context, code, msg string, data interface{}) {
}

func (r *customResponse) ParamError(c *gin.Context, err error) {
	r.response(c, http.StatusOK, "1002", http.StatusText(http.StatusBadRequest), struct{}{})
}

func (r *customResponse) Error(c *gin.Context, err error) bool {
	//r.response(c, http.StatusOK, "1001", "处理中", struct{}{})
	////e := errcode.ParseError(err)
	st, _ := status.FromError(err)
	r.response(c, http.StatusOK, strconv.Itoa(int(st.Code())), st.Message(), struct{}{})
	return false
}
