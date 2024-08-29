## grpc server

Generic grpc server.

### Example of use

```go
	import "github.com/18721889353/sunshine/pkg/grpc/server"

	port := 8282
	registerFn := func(s *grpc.Server) {
		pb.RegisterGreeterServer(s, &greeterServer{})
	}
	
	server.Run(port, registerFn,
		//server.WithSecure(credentials),
		//server.WithUnaryInterceptor(unaryInterceptors...),
		//server.WithStreamInterceptor(streamInterceptors...),
		//server.WithServiceRegister(func() {}),
	)

	select{}
```

Examples of practical use https://github.com/18721889353/grpc_examples/blob/main/usage/server/main.go
