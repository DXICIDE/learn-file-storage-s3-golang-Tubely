package main

import (
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
}

func fileExtensionMaker(contentType string) string {
	content := strings.Split(contentType, "/")
	return content[1]
}

func (cfg apiConfig) createFilePath(base64str string, fileExtension string) (*os.File, error) {
	fullpath := filepath.Join(cfg.assetsRoot, fmt.Sprintf("%v.%v", base64str, fileExtension))
	dst, err := os.Create(fullpath)

	return dst, err
}

func copyFileToDst(dst *os.File, file multipart.File) error {
	_, err := io.Copy(dst, file)
	return err
}

func (cfg apiConfig) makeURL(base64str string, fileExtension string) string {
	url := fmt.Sprintf("http://localhost:%v/assets/%v.%v", cfg.port, base64str, fileExtension)
	return url
}
