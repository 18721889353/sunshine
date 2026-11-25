package conf

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

var c = make(map[string]interface{})

func TestShow(t *testing.T) {
	t.Log(Show(c))
	t.Log(Show(make(chan string)))
}

func Test_replaceDSN(t *testing.T) {
	dsn := "default:123456@192.168.3.37:6379/0"
	t.Log(replaceDSN(dsn))

	dsn = "default:123456:192.168.3.37:6379/0"
	t.Log(replaceDSN(dsn))
}

func Test_hideSensitiveFields(t *testing.T) {
	var keywords []string
	keywords = append(keywords, `"dsn"`, `"password"`, `"name"`)
	str := Show(c, keywords...)

	fmt.Printf("%s", hideSensitiveFields(str))

	str = "\ndefault:123456@192.168.3.37:6379/0\n"
	fmt.Printf("%s", hideSensitiveFields(str))
}

// test listening for configuration file updates
func TestParse(t *testing.T) {
	conf := make(map[string]interface{})

	reloads := []func(){
		func() {
			fmt.Println("close and reconnect mysql")
			fmt.Println("close and reconnect redis")
		},
	}

	err := Parse("test.yml", &conf, reloads...)
	if err != nil {
		t.Error(err)
		return
	}

	time.Sleep(time.Second)
	content, _ := os.ReadFile("test.yml")
	contentChange := append(content, byte('#'))
	time.Sleep(time.Millisecond * 100)
	_ = os.WriteFile("test.yml", contentChange, 0666) // change file
	time.Sleep(time.Millisecond * 100)
	_ = os.WriteFile("test.yml", content, 0666) // recovery documents
	time.Sleep(time.Millisecond * 100)
}

func TestParseErr(t *testing.T) {
	// result error test
	err := Parse("test.yml", nil)
	t.Log(err)

	// not found error test
	err = Parse("notfound.yml", &c)
	t.Log(err)
}

func TestParseConfigData(t *testing.T) {
	conf := make(map[string]interface{})

	data, err := os.ReadFile("test.yml")
	if err != nil {
		t.Error(err)
		return
	}
	err = ParseConfigData(data, "yaml", &conf)
	if err != nil {
		t.Error(err)
		return
	}
	t.Log(Show(conf))
}

// TestShow_WebhookURLRedacted 守护：webhookURL 含企微机器人 key，
// /config 端点暴露配置时必须脱敏（新增默认敏感字段，防 key 泄露被劫持）。
func TestShow_WebhookURLRedacted(t *testing.T) {
	cfg := struct {
		WebhookURL string `json:"webhookURL"`
		Interval   int    `json:"intervalSec"`
	}{
		WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=SECRET_KEY_123",
		Interval:   60,
	}
	out := Show(cfg)
	if strings.Contains(out, "SECRET_KEY_123") {
		t.Errorf("webhookURL 的 key 必须脱敏, got %q", out)
	}
	if !strings.Contains(out, "******") {
		t.Errorf("脱敏后应有 ****** 标记, got %q", out)
	}
	if !strings.Contains(out, "60") {
		t.Errorf("非敏感字段应保留, got %q", out)
	}
}
