package nacos

import (
	"context"

	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/model"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

var _ registry.Watcher = &watcher{}

type watcher struct {
	serviceName string
	groupName   string
	clusterName string
	scheme      string
	client      naming_client.INamingClient

	ctx    context.Context
	cancel context.CancelFunc
	ch     chan []*registry.ServiceInstance
}

func (w *watcher) Next() ([]*registry.ServiceInstance, error) {
	select {
	case <-w.ctx.Done():
		return nil, w.ctx.Err()
	case instances := <-w.ch:
		return instances, nil
	}
}

func (w *watcher) Stop() error {
	w.cancel()
	return w.client.Unsubscribe(&vo.SubscribeParam{
		ServiceName: w.serviceName,
		GroupName:   w.groupName,
	})
}

// callback 是 Nacos Subscribe 回调函数，将 model.Instance 转换为 registry.ServiceInstance 并发送到 channel。
func (w *watcher) callback(services []model.Instance, err error) {
	if err != nil {
		return
	}

	instances := instancesToServiceInstances(services, w.scheme)

	// context 已取消时直接丢弃
	if w.ctx.Err() != nil {
		logger.WarnWithCtx(w.ctx, "[nacos watcher] context done, skip callback")
		return
	}

	select {
	case w.ch <- instances:
	default:
		logger.WarnWithCtx(w.ctx, "[nacos watcher] channel full, drop update")
	}
}
