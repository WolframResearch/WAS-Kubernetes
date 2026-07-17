package assets

import (
	"io/fs"
	"os"
)

// nativeFS wraps os.DirFS. Exists as a named type so we can test it
// independently of localDirFS's error wrapping.
type nativeFS string

func (n nativeFS) Open(name string) (fs.File, error) {
	return os.DirFS(string(n)).Open(name)
}
