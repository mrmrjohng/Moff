package database

import "strings"

const (
	duplicateKeyErrString = "duplicate key"
)

//IsDuplicateKeyErr 返回是否为唯一键冲突错误.
func IsDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), duplicateKeyErrString)
}
