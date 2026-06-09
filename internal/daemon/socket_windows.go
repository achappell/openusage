//go:build windows

package daemon

import "os"

// socketFileLooksLikeSocket reports whether an existing path could be a unix
// domain socket. On Windows an AF_UNIX socket path is materialized as a regular
// file / reparse point and never reports os.ModeSocket, so mode can't identify
// it. Treat any non-directory as a possible (stale) socket and let the
// dial-probe in EnsureSocketPathAvailable decide whether a live daemon owns it.
func socketFileLooksLikeSocket(info os.FileInfo) bool {
	return !info.IsDir()
}
