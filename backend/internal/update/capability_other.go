//go:build !linux

package update

// copyCapability is a no-op off Linux: macOS binaries carry their code
// signature inside the file bytes, so a verbatim-extracted binary keeps it.
func copyCapability(src, dst string) error {
	return nil
}
