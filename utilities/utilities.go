package utilities

import (
	"crypto/sha1"
	"encoding/base64"
	"io"
	"os"
	"strings"

	"github.com/Kandamkolathy/immich-sync/client"
	"github.com/kardianos/service"
)

// Created so that multiple inputs can be accecpted
type ArrayFlags []string

func (i *ArrayFlags) String() string {
	// change this, this is just can example to satisfy the interface
	return "my string representation"
}

func (i *ArrayFlags) Set(value string) error {
	*i = append(*i, strings.TrimSpace(value))
	return nil
}

func FileExists(filename string) bool {
	// Use os.Stat to get file information
	_, err := os.Stat(filename)
	// If there's no error, the file exists
	if err == nil {
		return true
	}
	// If the error is because the file does not exist, return false
	if os.IsNotExist(err) {
		return false
	}
	// If there's another kind of error, you can handle it as needed
	return false
}

func GetFileSHAs(shaMap *[]client.ChecksumPair, path string, logger service.Logger) error {
	f, err := os.Open(path)
	if err != nil {
		logger.Info("Failed to open file for SHA")
		logger.Info(err)
		return err
	}

	defer f.Close()

	h := sha1.New()

	if _, err := io.Copy(h, f); err != nil {
		logger.Info("SHA byte copy failed")
		logger.Info(err)
		return err
	}
	*shaMap = append(*shaMap, client.ChecksumPair{Checksum: base64.StdEncoding.EncodeToString(h.Sum(nil)), ID: path})
	return nil
}
