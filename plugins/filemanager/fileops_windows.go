//go:build windows

package filemanager

import "io/fs"

func populateOwnerGroup(_ fs.FileInfo, _ *FileInfo) {}
