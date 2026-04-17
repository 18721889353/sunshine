package utils

import (
	"testing"
)

func TestAdaptiveMysqlDsn(t *testing.T) {
	mysqlDsns := []string{
		"root:123456@(192.168.3.37:3306)/account",
		"mysql://root:123456@(192.168.3.37:3306)/account",
	}

	for _, v := range mysqlDsns {
		dsn := AdaptiveMysqlDsn(v)
		t.Log(dsn)
	}
}
