package jwt

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGenerateCustomToken(t *testing.T) {
	var (
		id     uint64 = 20
		name   string = "admin"
		age    int    = 10
		fields        = map[string]any{"id": id, "name": name, "age": age, "fuck": []string{"11", "22"}}
	)

	Init(
		WithSigningKey("qcGnoQoYKn9bWLFWjk7MRuGIKqLbnWFh"),
	)
	token, err := GenerateToken("uid", "name", fields)
	fmt.Println(token, err)
	//time.Sleep(time.Second * 5)
	//assert.NoError(t, err)
	token = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3NjYyMTUwMDcsInN1YiI6MTAsIm5iZiI6MTc2NTk1NTgwNywiYXVkIjoiZGVhbGVyOmFwaTp0b2tlbjo1Yjk0ZWRkNTVhYjE4OTlkZDVlY2QxNjM2ODM3N2M3OSIsImlhdCI6MTc2NTk1NTgwNywianRpIjoiMTA4ZTgyMDViYzZjOGU2NzI5MDBjZTUzZDkxNTkxYjQiLCJpc3MiOiJqc3J4X3N0Iiwic3RhdHVzIjoxLCJkYXRhIjp7ImlkIjoxLCJhcHBfaWQiOiJ6dDYwOThlYWY1YjJkMjkiLCJhcHBfc2VjcmV0IjoiYjgxZjhjNDE5OTdiODkxMGZmN2JmNjMxYTc0ZDY1ZWUiLCJuYW1lIjoi5YmN56uv5rWL6K-V5ZWG5oi3IiwicGhvbmUiOiIxNTYwNTI4NDAyOCIsImlwX3doaXRlIjoiKiJ9fQ.LqtWmrusA0wATtR6Ys2WO2TRXq0FOhPOIQ1i6xOB_mg"
	claims, err := ParseToken(token)
	fmt.Println(claims, err)
	assert.NoError(t, err)
	ip, ok := claims.Fields["ip_white"]
	fmt.Println(ip, ok)

}

func TestParseCustomToken(t *testing.T) {
	fields := map[string]any{"id": 123, "foo": "bar"}
	opt = nil
	v, err := ParseToken("token")
	assert.Error(t, err)

	Init(
		WithSigningKey("123456"),
		WithExpire(time.Second),
		WithSigningMethod(HS512),
		WithIssuer("test_issuer"),
		WithSubject("test_subject"),
		WithAudience([]string{"test_audience"}),
		WithID("test_id"),
		WithNotBefore(time.Now().Add(-time.Hour)), // 1 hour ago
	)

	// success
	token, err := GenerateToken("", "", fields)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(token)
	v, err = ParseToken(token)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(v)

	// invalid token format
	token2 := "xxx.xxx.xxx"
	v, err = ParseToken(token2)
	assert.Error(t, err)

	// signature failure
	token3 := token + "xxx"
	v, err = ParseToken(token3)
	assert.Error(t, err)

	// token has expired
	token, err = GenerateToken("", "", fields)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second * 2)
	v, err = ParseToken(token)
	assert.True(t, errors.Is(err, ErrTokenExpired))
}
