//go:build !windows

package project

import "os"

func replaceDocumentFile(source, target string) error { return os.Rename(source, target) }
