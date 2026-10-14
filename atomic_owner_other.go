//go:build !unix

package main

import "os"

func keepOwner(*os.File, os.FileInfo) {}
