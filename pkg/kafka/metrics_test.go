package kafka

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestObserveKafkaPublishConsume 生产/消费指标按 result 口径记录（ok/error）。
// 用 CollectAndCount 验证样本被 emit（Histogram vec 无样本不 emit）。
func TestObserveKafkaPublishConsume(t *testing.T) {
	beforePub := testutil.CollectAndCount(kafkaPublishDuration)
	beforeCon := testutil.CollectAndCount(kafkaConsumeDuration)

	observeKafkaPublish("t1", errors.New("boom"), time.Millisecond)
	observeKafkaConsume("t1", nil, time.Millisecond)

	if got := testutil.CollectAndCount(kafkaPublishDuration); got <= beforePub {
		t.Errorf("publish 样本应增加, got %d", got)
	}
	if got := testutil.CollectAndCount(kafkaConsumeDuration); got <= beforeCon {
		t.Errorf("consume 样本应增加, got %d", got)
	}
}
