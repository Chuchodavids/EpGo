package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"fmt"
	"io"
	"strings"
)

// SHA1 : SHA1
func SHA1(str string) (strSHA1 string) {

	h := sha1.New()
	io.WriteString(h, str)
	strSHA1 = strings.ToLower(fmt.Sprintf("% x", h.Sum(nil)))
	strSHA1 = strings.Replace(strSHA1, " ", "", -1)

	return
}

// ContainsString : Get string position in slice
func ContainsString(slice []string, e string) int {
	for i, a := range slice {
		if a == e {
			return i
		}
	}
	return -1
}

func gUnzip(data []byte) (res []byte, err error) {

	b := bytes.NewBuffer(data)

	r, err := gzip.NewReader(b)
	if err != nil {
		return
	}
	defer r.Close()

	var resB bytes.Buffer
	_, err = resB.ReadFrom(r)
	if err != nil {
		return
	}

	res = resB.Bytes()

	return
}

func decodeSDPayload(data []byte) ([]byte, error) {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		return gUnzip(data)
	}
	return data, nil
}
