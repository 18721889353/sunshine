## grpc client

Generic grpc client.

### Example of use

```go
	import "github.com/18721889353/sunshine/pkg/grpc/client"

	conn, err := client.Dial(context.Background(), "127.0.0.1:8282",
		//client.WithServiceDiscover(builder),
		//client.WithLoadBalance(),
		//client.WithSecure(credentials),
		//client.WithUnaryInterceptor(unaryInterceptors...),
		//client.WithStreamInterceptor(streamInterceptors...),
	)
```

Examples of practical use https://github.com/18721889353/grpc_examples/blob/main/usage/client/main.go
