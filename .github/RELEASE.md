## Change log

1. Adjust errcode package, support custom error message.

example:

```go
//http code
    // code(10003) and message
    ecode.InvalidParams.Err("custom error message")
    // code(400) and message
    ecode.InvalidParams.ErrToHTTP("custom error message")

// grpc code
    // code(30003) and message
    ecode.StatusInvalidParams.Err("custom error message")
    // code(3) and message
    ecode.StatusInvalidParams.ToRPCErr("custom error message")
    // code(30003) and message, use in grpc-gateway
    ecode.StatusInvalidParams.ErrToHTTP("custom error message")
```

2. Adjust some code. 
3. Fix logging bug
4. snake case style- Modify the scripts in the large repository type so that the web service and grpc service code generated based on SQL can automatically complete the missing parts.
5. - Modified the code directory structure for large repository services, making the `api` and `third_party` directories shared among all services, while keeping other directories unchanged.
