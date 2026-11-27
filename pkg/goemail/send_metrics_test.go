package goemail

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestObserveEmailSend 外呼指标按 result 口径记录（Histogram 是 Observer，用 CollectAndCount 验证样本）。
func TestObserveEmailSend(t *testing.T) {
	before := testutil.CollectAndCount(emailSendDuration)

	observeEmailSend("smtp", errors.New("boom"), time.Millisecond)
	observeEmailSend("aliyun", nil, time.Millisecond)

	if got := testutil.CollectAndCount(emailSendDuration); got <= before {
		t.Errorf("email 样本应增加, got %d", got)
	}
}
