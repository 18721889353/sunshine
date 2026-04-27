// Package upload 提供文件上传功能。
package upload

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/bwmarrin/snowflake"
)

// CosUploaderOptions 上传配置选项
type CosUploaderOptions struct {
	expireTime      int64         // 签名过期时间（秒）
	fileDirPrefix   string        // 文件存储路径前缀
	maxFileSize     int64         // 最大文件大小（字节）
	policyCondition []interface{} // 策略条件
	prefixURL       string        //域名前缀

}

// CosUploaderOption 配置选项函数
type CosUploaderOption func(*CosUploaderOptions)

// 默认配置
func defaultCosUploaderOptions() *CosUploaderOptions {
	return &CosUploaderOptions{
		expireTime:      5,               // 默认过期时间 120 秒
		fileDirPrefix:   "image",         // 默认路径前缀
		maxFileSize:     5 * 1024 * 1024, // 默认最大文件 10MB
		policyCondition: []interface{}{"content-length-range", 1, 5 * 1024 * 1024},
		prefixURL:       "",
	}
}

// WithExpireTime 设置签名过期时间
func WithExpireTime(seconds int64) CosUploaderOption {
	return func(o *CosUploaderOptions) {
		o.expireTime = seconds
	}
}

// WithFileDirPrefix 设置文件存储路径前缀
func WithFileDirPrefix(prefix string) CosUploaderOption {
	return func(o *CosUploaderOptions) {
		o.fileDirPrefix = prefix
	}
}

// WithMaxFileSize 设置最大文件大小
func WithMaxFileSize(size int64) CosUploaderOption {
	return func(o *CosUploaderOptions) {
		o.maxFileSize = size
		o.policyCondition = []interface{}{"content-length-range", 1, size}
	}
}

// WithPrefixURL 设置文件存储路径前缀
func WithPrefixURL(prefixURL string) CosUploaderOption {
	return func(o *CosUploaderOptions) {
		o.prefixURL = prefixURL
	}
}

// cosUploader 改造后支持动态配置
type cosUploader struct {
	Bucket    string
	Region    string
	SecretID  string
	SecretKey string
	SnowNode  *snowflake.Node
	opts      *CosUploaderOptions // 持有配置选项
}

// NewCosUploader 支持传入选项参数
func NewCosUploader(cosUploaderInfo *CosUploaderInfo, opts ...CosUploaderOption) *cosUploader {
	options := defaultCosUploaderOptions()
	for _, opt := range opts {
		opt(options)
	}
	return &cosUploader{
		Bucket:    cosUploaderInfo.Bucket,
		Region:    cosUploaderInfo.Region,
		SecretID:  cosUploaderInfo.SecretID,
		SecretKey: cosUploaderInfo.SecretKey,
		SnowNode:  cosUploaderInfo.SnowNode,
		opts:      options,
	}
}

// CosPrefixUrl 返回 COS 域名前缀
func (c *cosUploader) cosPrefixURL() string {
	if c.opts.prefixURL != "" {
		return c.opts.prefixURL
	}
	return fmt.Sprintf("https://%s.cos.%s.myqcloud.com", c.Bucket, c.Region)
}

// GetFileUploadRecPath 获取文件上传路径
func (c *cosUploader) getFileUploadRecPath(fileName string) (string, error) {
	fileExt := path.Ext(fileName)
	if fileExt == "" {
		return "", fmt.Errorf("文件扩展名不能为空")
	}
	dirPath := fmt.Sprintf("/%s/%s/", c.opts.fileDirPrefix, time.Now().Format("20060102"))
	unionName := c.SnowNode.Generate().String()
	saveName := unionName + fileExt
	return dirPath + saveName, nil
}

// CosCredentials 生成 COS 凭证
func (c *cosUploader) cosCredentials() map[string]string {
	secretKey := []byte(c.SecretKey)
	startTime := time.Now().Unix()
	endTime := startTime + c.opts.expireTime // 使用配置的过期时间

	expiration := time.Unix(endTime, 0).UTC().Format(time.RFC3339)
	expiration = strings.Replace(expiration, "+00:00", "Z", 1)

	qKeyTime := fmt.Sprintf("%d;%d", startTime, endTime)

	// 构建策略数据
	policyData := map[string]interface{}{
		"expiration": expiration,
		"conditions": []interface{}{
			map[string]string{"q-sign-algorithm": "sha1"},
			map[string]string{"q-ak": c.SecretID},
			map[string]string{"q-sign-time": qKeyTime},
			c.opts.policyCondition, // 使用配置的策略条件
		},
	}

	policy, err := json.Marshal(policyData)
	if err != nil {
		return map[string]string{}
	}

	h := hmac.New(sha1.New, secretKey)
	h.Write([]byte(qKeyTime))
	signKey := []byte(hex.EncodeToString(h.Sum(nil)))

	sha1Hash := sha1.Sum(policy)
	stringToSign := hex.EncodeToString(sha1Hash[:])

	h2 := hmac.New(sha1.New, signKey)
	h2.Write([]byte(stringToSign))
	qSignatureString := hex.EncodeToString(h2.Sum(nil))

	return map[string]string{
		"q-sign-algorithm": "sha1",
		"q-ak":             c.SecretID,
		"q-key-time":       qKeyTime,
		"q-signature":      qSignatureString,
		"policy":           base64.StdEncoding.EncodeToString(policy),
	}
}

// UploadCosPrepareData 生成上传所需的预签名信息
func (c *cosUploader) UploadCosPrepareData(fileName string) (*CosInfo, error) {
	savePath, err := c.getFileUploadRecPath(fileName)
	if err != nil {
		return nil, err
	}
	credentials := c.cosCredentials()
	return &CosInfo{
		PrefixURL:      c.cosPrefixURL(),
		SavePath:       savePath,
		QSignAlgorithm: credentials["q-sign-algorithm"],
		QAk:            credentials["q-ak"],
		QKeyTime:       credentials["q-key-time"],
		QSignature:     credentials["q-signature"],
		Policy:         credentials["policy"],
	}, nil
}

// CosInfo 返回的 COS 上传信息结构
type CosInfo struct {
	PrefixURL      string
	SavePath       string
	QSignAlgorithm string
	QAk            string
	QKeyTime       string
	QSignature     string
	Policy         string
}

// CosUploaderInfo 初始化上传器所需的基础信息
type CosUploaderInfo struct {
	Bucket    string
	Region    string
	SecretID  string
	SecretKey string
	SnowNode  *snowflake.Node
}
