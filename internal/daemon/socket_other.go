//go:build !windows

package daemon

import "os"

// socketFileLooksLikeSocket reports whether an existing path could be a unix
// domain socket. On Unix the file mode carries os.ModeSocket.
func socketFileLooksLikeSocket(info os.FileInfo) bool {
	return info.Mode()&os.ModeSocket != 0
}
