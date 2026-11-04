//go:build unix

package admin

import "syscall"

// diskUsage returns used and total bytes of the filesystem holding path.
func diskUsage(path string) (used, total int64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bs := int64(st.Bsize)         //nolint:unconvert,gosec // field type differs by OS
	total = int64(st.Blocks) * bs //nolint:gosec // block counts fit int64
	free := int64(st.Bavail) * bs //nolint:gosec // block counts fit int64
	return total - free, total, nil
}
