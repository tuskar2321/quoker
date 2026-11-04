//go:build !unix

package admin

import "errors"

func diskUsage(string) (used, total int64, err error) {
	return 0, 0, errors.New("disk usage is not supported on this OS")
}
