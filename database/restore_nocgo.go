//go:build !cgo

package database

import "github.com/admin8800/s-ui/util/common"

func replaceSQLiteDatabase(string) error {
	return common.NewError("database restore requires a CGO-enabled SQLite build")
}
