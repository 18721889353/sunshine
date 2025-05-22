package upload

import (
	"github.com/bwmarrin/snowflake"
	"github.com/stretchr/testify/assert"
	"testing"
)

func Test_cosUploader_UploadCosPrepareData(t *testing.T) {
	node, err := snowflake.NewNode(1)
	if err != nil {
		assert.NoError(t, err)
		return
	}
	token, err := NewCosUploader(&CosUploaderInfo{
		Bucket:    "test",
		Region:    "test",
		SecretID:  "test",
		SecretKey: "test",
		SnowNode:  node,
	}).UploadCosPrepareData("aa.jpg")
	if err != nil {
		assert.NoError(t, err)
		return
	}
	t.Log(token)
}

func Benchmark_cosUploader_UploadCosPrepareData(b *testing.B) {
	node, _ := snowflake.NewNode(1)
	uploader := NewCosUploader(&CosUploaderInfo{
		Bucket:    "test",
		Region:    "test",
		SecretID:  "test",
		SecretKey: "test",
		SnowNode:  node,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := uploader.UploadCosPrepareData("aa.jpg")
		if err != nil {
			b.Fatal(err)
		}
	}
}
