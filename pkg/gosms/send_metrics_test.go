package gosms

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestObserveSMSSend 短信外呼指标按 result 口径记录。
func TestObserveSMSSend(t *testing.T) {
	before := testutil.CollectAndCount(smsSendDuration)

	observeSMSSend(errors.New("boom"), time.Millisecond)

	if got := testutil.CollectAndCount(smsSendDuration); got <= before {
		t.Errorf("sms 样本应增加, got %d", got)
	}
}
