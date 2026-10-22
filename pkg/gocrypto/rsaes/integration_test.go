//go:build integration

package rsaes

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// 跨实现互操作测试：本包存在的唯一理由是「与别的实现互通」——Java 的
// RSA/ECB/PKCS1Padding、OpenSSL 的 rsa_padding_mode:pkcs1 走的是同一套 RFC 8017 填充。
// 只用 Go 自己往返证明不了线上对端能解开，所以这里请 openssl 当外部对端：
// 它加密的密文本包要能解，本包加密的密文它要能解，两个方向都过才算兼容。
//
// 依赖的是本机可执行文件而非网络服务，没有凭据要配置，因此本文件不读 .env，
// 只保留 TestMain 总时长上限（防子进程挂死把 go test 拖住）。
// 未安装 openssl 时自动 Skip；可用 OPENSSL_BIN 指定可执行文件路径。

const (
	// defaultIntegrationTimeout 集成测试总时长的兜底上限。
	defaultIntegrationTimeout = 120 * time.Second
	// opensslCommandTimeout 单次 openssl 子调用的上限。
	// 为何只能固定等待：openssl 是外部进程，没有「已完成」的可订阅信号，只能用 context 兜住。
	// 取值依据：本机实测单次调用为毫秒级（见 README 实测汇总），60s 是数量级余量。
	// 为何不会因而 flaky：判定靠子进程退出码与还原字节比对，超时只会把挂死暴露成失败而不是卡住。
	opensslCommandTimeout = 60 * time.Second
	// opensslKeyBits 互操作用密钥长度，取生产默认档位。
	opensslKeyBits = 2048
	// opensslMaxPlainLen 2048 位密钥下 PKCS#1 v1.5 的明文上界 k-11，开销取自 emFixedOverhead。
	opensslMaxPlainLen = opensslKeyBits/8 - emFixedOverhead
)

// TestMain 限制集成测试总时长，避免外部子进程挂死导致 go test 永不返回。
func TestMain(m *testing.M) {
	ch := make(chan int, 1)
	go func() {
		ch <- m.Run()
	}()

	select {
	case code := <-ch:
		os.Exit(code)
	case <-time.After(defaultIntegrationTimeout):
		fmt.Fprintf(os.Stderr,
			"FATAL: 集成测试超过 %s 超时（可能是 openssl 子进程挂死）\n", defaultIntegrationTimeout)
		os.Exit(1)
	}
}

// opensslPeer 是充当外部对端的 openssl 及其配套密钥文件。
type opensslPeer struct {
	bin      string // openssl 可执行文件路径
	pubPath  string // PKIX 公钥 PEM，供 -pubin 使用
	privPath string // PKCS#1 私钥 PEM，与 gocrypto 默认格式一致
	priv     *rsa.PrivateKey
}

// newOpenSSLPeer 生成一次性密钥并落成 openssl 能读的 PEM 文件；未安装 openssl 时跳过。
func newOpenSSLPeer(t *testing.T) *opensslPeer {
	t.Helper()

	name := os.Getenv("OPENSSL_BIN")
	if name == "" {
		name = "openssl"
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("未找到 openssl（%v），跳过跨实现互操作测试；可用 OPENSSL_BIN 指定路径", err)
	}

	priv, err := rsa.GenerateKey(rand.Reader, opensslKeyBits)
	if err != nil {
		t.Fatalf("生成 %d 位测试密钥失败: %v", opensslKeyBits, err)
	}

	dir := t.TempDir()
	peer := &opensslPeer{bin: bin, priv: priv, pubPath: filepath.Join(dir, "pub.pem"), privPath: filepath.Join(dir, "priv.pem")}

	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("公钥 DER 编码失败: %v", err)
	}
	writeTestFile(t, peer.pubPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	writeTestFile(t, peer.privPath, pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	}))
	return peer
}

// writeTestFile 写入临时文件，权限收到 0600（含私钥内容）。
func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("写 %s 失败: %v", path, err)
	}
}

// run 在超时保护下执行 openssl 子命令，失败时把它的输出一起打出来便于定位。
func (p *opensslPeer) run(t *testing.T, args ...string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), opensslCommandTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, p.bin, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("openssl %v 失败: %v\n输出: %s", args, err, out)
	}
}

// encrypt 让 openssl 用公钥加密 plaintext，返回它产出的密文。
func (p *opensslPeer) encrypt(t *testing.T, plaintext []byte) []byte {
	t.Helper()

	in := filepath.Join(t.TempDir(), "plain.bin")
	ct := filepath.Join(t.TempDir(), "cipher.bin")
	writeTestFile(t, in, plaintext)

	// 显式指定 pkcs1 填充，不依赖不同 openssl 版本的默认值。
	p.run(t, "pkeyutl", "-encrypt", "-pubin", "-inkey", p.pubPath,
		"-pkeyopt", "rsa_padding_mode:pkcs1", "-in", in, "-out", ct)

	data, err := os.ReadFile(ct)
	if err != nil {
		t.Fatalf("读 openssl 密文失败: %v", err)
	}
	return data
}

// decrypt 让 openssl 用私钥解密 ciphertext，返回它还原出的明文。
func (p *opensslPeer) decrypt(t *testing.T, ciphertext []byte) []byte {
	t.Helper()

	ct := filepath.Join(t.TempDir(), "cipher.bin")
	out := filepath.Join(t.TempDir(), "plain.bin")
	writeTestFile(t, ct, ciphertext)

	p.run(t, "pkeyutl", "-decrypt", "-inkey", p.privPath, "-in", ct, "-out", out)

	plaintext, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("读 openssl 明文失败: %v", err)
	}
	return plaintext
}

func TestIntegration_OpenSSLDecryptsOurCiphertext(t *testing.T) {
	peer := newOpenSSLPeer(t)

	cases := []struct {
		name string
		msg  []byte
	}{
		{"ASCII 明文", []byte("hello from sunshine rsaes")},
		{"中文明文", []byte("阳光框架 RSA 跨实现互通")},
		{"含零字节的明文", []byte("a\x00b\x01c")},
		{"空明文", []byte{}},
		{"达到上界的明文", bytes.Repeat([]byte{0x7f}, opensslMaxPlainLen)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext, err := Encrypt(&peer.priv.PublicKey, tc.msg)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			if got := peer.decrypt(t, ciphertext); !bytes.Equal(got, tc.msg) {
				t.Fatalf("openssl 还原结果不符: got %q, want %q", got, tc.msg)
			}
		})
	}
}

func TestIntegration_OpenSSLEncryptsForOurDecrypt(t *testing.T) {
	peer := newOpenSSLPeer(t)

	cases := []struct {
		name string
		msg  []byte
	}{
		{"ASCII 明文", []byte("openssl -> sunshine rsaes")},
		{"中文明文", []byte("反向互通：openssl 加密，本包解密")},
		{"达到上界的明文", bytes.Repeat([]byte{0x2a}, opensslMaxPlainLen)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Decrypt(peer.priv, peer.encrypt(t, tc.msg))
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}
			if !bytes.Equal(got, tc.msg) {
				t.Fatalf("got %q, want %q", got, tc.msg)
			}
		})
	}
}

// TestIntegration_OpenSSLChecksOurKeyPEM 确认 Go 生成的 PKCS#1 私钥 PEM 与公钥 PEM
// 都能被 openssl 直接读入——gorsa/gocrypto 的密钥来源是配置文件里的 PEM 文本，
// 对端工具读不了就等于接不上现成运维流程。
func TestIntegration_OpenSSLChecksOurKeyPEM(t *testing.T) {
	peer := newOpenSSLPeer(t)

	peer.run(t, "rsa", "-in", peer.privPath, "-check", "-noout")
	peer.run(t, "rsa", "-pubin", "-in", peer.pubPath, "-noout")
}
