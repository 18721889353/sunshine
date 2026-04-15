package interceptor

import (
	"testing"
	"time"
)

func TestUnaryClientLog(t *testing.T) {
	addr := newUnaryRPCServer()
	time.Sleep(time.Millisecond * 200)
	cli := newUnaryRPCClient(addr,
		UnaryClientRequestID(),
		UnaryClientLog(WithReplaceGRPCLogger()),
	)
	_ = sayHelloMethod(cli)
}

func TestUnaryServerLog(t *testing.T) {
	addr := newUnaryRPCServer(
		UnaryServerRequestID(),
		UnaryServerLog(WithReplaceGRPCLogger()),
		UnaryServerSimpleLog(WithReplaceGRPCLogger()),
	)
	time.Sleep(time.Millisecond * 200)
	cli := newUnaryRPCClient(addr)
	_ = sayHelloMethod(cli)
}

func TestStreamClientLog(t *testing.T) {
	addr := newStreamRPCServer()
	time.Sleep(time.Millisecond * 200)
	cli := newStreamRPCClient(addr,
		StreamClientRequestID(),
		StreamClientLog(WithReplaceGRPCLogger()),
	)
	_ = discussHelloMethod(cli)
	time.Sleep(time.Millisecond)
}

func TestUnaryServerLog_ignore(t *testing.T) {
	addr := newUnaryRPCServer(
		UnaryServerLog(
			WithLogFields(map[string]interface{}{"foo": "bar"}),
			WithLogIgnoreMethods("/api.user.v1.user/GetByID"),
		),
	)
	time.Sleep(time.Millisecond * 200)
	cli := newUnaryRPCClient(addr)
	_ = sayHelloMethod(cli)
}

func TestStreamServerLog(t *testing.T) {
	addr := newStreamRPCServer(
		StreamServerRequestID(),
		StreamServerLog(
			WithReplaceGRPCLogger(),
			WithLogFields(map[string]interface{}{}),
		),
		StreamServerSimpleLog(
			WithReplaceGRPCLogger(),
			WithLogFields(map[string]interface{}{}),
		),
	)
	time.Sleep(time.Millisecond * 200)
	cli := newStreamRPCClient(addr)
	_ = discussHelloMethod(cli)
	time.Sleep(time.Millisecond)
}

// ----------------------------------------------------------------------------------------

func TestNilLog(t *testing.T) {
	UnaryClientLog()
	StreamClientLog()
	UnaryServerLog()
	UnaryServerSimpleLog()
	StreamServerLog()
	StreamServerSimpleLog()
}
