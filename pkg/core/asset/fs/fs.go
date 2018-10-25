// Copyright 2015-present Oursky Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fs

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/skygeario/skygear-server/pkg/core/asset"
)

// AssetStore implements Store by storing files on file system
type AssetStore struct {
	dir    string
	prefix string
	secret string
	public bool
	logger *logrus.Entry
}

// NewAssetStore creates a new file asset store
func NewAssetStore(dir, prefix, secret string, public bool, logger *logrus.Entry) *AssetStore {
	return &AssetStore{dir, prefix, secret, public, logger}
}

// GetFileReader returns a reader for reading files
func (s *AssetStore) GetFileReader(name string) (io.ReadCloser, error) {
	path := filepath.Join(s.dir, name)
	return os.Open(filepath.Clean(path))
}

// GetRangedFileReader returns a reader for reading files within
// the specified byte range
func (s *AssetStore) GetRangedFileReader(name string, fileRange asset.FileRange) (
	*asset.FileRangedGetResult,
	error,
) {
	path := filepath.Join(s.dir, name)

	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	fileStat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	fileSize := fileStat.Size()
	if fileRange.From >= fileSize {
		return nil, asset.FileRangeNotAcceptedError{fileRange}
	}

	if _, err = file.Seek(fileRange.From, 0); err != nil {
		return nil, err
	}

	acceptedRange := asset.FileRange{
		From: fileRange.From,
		To:   fileRange.To,
	}

	if acceptedRange.To > fileSize-1 {
		acceptedRange.To = fileSize - 1
	}

	return &asset.FileRangedGetResult{
		ReadCloser:    file,
		AcceptedRange: acceptedRange,
		TotalSize:     fileSize,
	}, nil
}

// PutFileReader stores a file from reader onto file system
func (s *AssetStore) PutFileReader(name string, src io.Reader, length int64, contentType string) error {
	path := filepath.Join(s.dir, name)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	written, err := io.Copy(f, src)
	if err != nil {
		return err
	}

	if written != length {
		return fmt.Errorf("got written %d bytes, expect %d", written, length)
	}

	return nil
}

// GeneratePostFileRequest return a PostFileRequest for uploading asset
func (s *AssetStore) GeneratePostFileRequest(name string, contentType string, length int64) (*asset.PostFileRequest, error) {
	return &asset.PostFileRequest{
		Action: "/files/" + name,
	}, nil
}

// SignedURL returns a signed url with expiry date
func (s *AssetStore) SignedURL(name string) (string, error) {
	if !s.IsSignatureRequired() {
		return fmt.Sprintf("%s/%s", s.prefix, name), nil
	}

	expiredAt := time.Now().Add(time.Minute * time.Duration(15))
	expiredAtStr := strconv.FormatInt(expiredAt.Unix(), 10)

	h := hmac.New(sha256.New, []byte(s.secret))
	io.WriteString(h, name)
	io.WriteString(h, expiredAtStr)

	buf := bytes.Buffer{}
	base64Encoder := base64.NewEncoder(base64.URLEncoding, &buf)
	base64Encoder.Write(h.Sum(nil))
	base64Encoder.Close()

	return fmt.Sprintf(
		"%s/%s?expiredAt=%s&signature=%s",
		s.prefix, name, expiredAtStr, buf.String(),
	), nil
}

// ParseSignature tries to parse the asset signature
func (s *AssetStore) ParseSignature(signed string, name string, expiredAt time.Time) (valid bool, err error) {
	base64Decoder := base64.NewDecoder(base64.URLEncoding, strings.NewReader(signed))
	remoteSignature, err := ioutil.ReadAll(base64Decoder)
	if err != nil {
		s.logger.Errorf("failed to decode asset url signature: %v", err)
		return false, errors.New("invalid signature")
	}

	h := hmac.New(sha256.New, []byte(s.secret))
	io.WriteString(h, name)
	io.WriteString(h, strconv.FormatInt(expiredAt.Unix(), 10))

	return hmac.Equal(remoteSignature, h.Sum(nil)), nil
}

// IsSignatureRequired indicates whether a signature is required
func (s *AssetStore) IsSignatureRequired() bool {
	return !s.public
}