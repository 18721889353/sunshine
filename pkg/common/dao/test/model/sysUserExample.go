// Package model sys_user_example table
package model

import (
	"github.com/18721889353/sunshine/pkg/sgorm"
)

// SysUserExample model
type SysUserExample struct {
	sgorm.Model `gorm:"embedded"` // embed id and time

	Name     string `gorm:"column:name;type:longtext;not null" json:"name"`
	Password string `gorm:"column:password;type:longtext;not null" json:"password"`
	Email    string `gorm:"column:email;type:longtext;not null" json:"email"`
	Phone    string `gorm:"column:phone;type:longtext;not null" json:"phone"`
	Avatar   string `gorm:"column:avatar;type:longtext;not null" json:"avatar"`
	Age      *int64 `gorm:"column:age;type:bigint(20)" json:"age"`
	Gender   *int64 `gorm:"column:gender;type:bigint(20)" json:"gender"`
	Status   *int64 `gorm:"column:status;type:bigint(20)" json:"status"`
	LoginAt  *int64 `gorm:"column:login_at;type:bigint(20)" json:"loginAt"`
}

// TableName table name
func (m *SysUserExample) TableName() string {
	return "sys_user_example"
}
