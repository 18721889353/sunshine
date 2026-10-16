## response

The wrapper gin returns json data in the same format.

## 配置驱动说明

本包是 `pkg/errcode` 全局 `ResponseFormat` 的适配层，响应体字段名、code/msg 内容与序列化类型均按配置文件 `response` 段实时生效（含热更新），与 gRPC 网关(http-pb)链路输出一致：

- `values.successCode`: 响应码的 JSON 类型跟随 YAML 写法——不带引号(`successCode: 0`)输出数字 `0`，带引号(`successCode: "0"`)输出字符串 `"0"`；留空默认数字 `0`
- `values.successMsg` / `values.doingMsg`: 成功提示与业务自定义码提示
- `extractCustomCode`: 从 data 中提取 `CustomCode` 字段（或 map 的 `code` 键）作为响应码并剥离该字段，提取值类型原样透传
- `fields.code/msg/data`: 响应体字段名自定义
- `nilDataAsEmptyArray`: nil/空 data 序列化为 `[]` 而非 `{}`

`Error`/`Output`/`Out` 的 body code 保持数字形态（业务错误码/HTTP 状态码），与既有前端约定一致。

<br>

## Example of use

- `Output`  return a compatible http status code.
- `Success` and `Error` return a uniform status code of 200, with a custom status code in data.code

all requests return a uniform json

```json
{
  "code": 0,
  "msg": "",
  "data": {}
}
```

```go
    // c is *gin.Context

    // return success
    response.Success(c)
    // return success and return data
    response.Success(c, gin.H{"users":users})

    // return failure
    response.Error(c, errcode.SendEmailErr)
    // returns a failure and returns the data
    response.Error(c,  errcode.SendEmailErr, gin.H{"user":user})
```