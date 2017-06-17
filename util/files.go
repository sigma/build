// Copyright 2015 The appc Authors
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

package util

import (
	"archive/tar"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/appc/spec/aci"
	"github.com/coreos/pkg/progressutil"
	rkttar "github.com/coreos/rkt/pkg/tar"
	"github.com/coreos/rkt/pkg/user"
)

func DownloadFile(uri string, insecure bool, w io.Writer) error {
	u, err := url.Parse(uri)
	if err != nil {
		return err
	}
	if u.Scheme == "http" && !insecure {
		return fmt.Errorf("Won't download from HTTP without --insecure")
	}
	name := filepath.Base(u.Path)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecure,
		},
	}
	client := &http.Client{Transport: tr}
	resp, err := client.Get(uri)
	if err != nil {
		return err
	}

	// If the server specified a content disposition, try to get the name from
	// there. Just a small cosmetic improvement.
	disphdr := resp.Header.Get("Content-Disposition")
	if disphdr != "" {
		_, params, err := mime.ParseMediaType(disphdr)
		if err == nil && params["filename"] != "" {
			name = params["filename"]
		}
	}

	copier := progressutil.NewCopyProgressPrinter()
	copier.AddCopy(resp.Body, name, resp.ContentLength, w)
	return copier.PrintAndWait(os.Stderr, 500*time.Millisecond, nil)
}

// RmAndMkdir will remove anything at path if it exists, and then create a
// directory at path.
func RmAndMkdir(path string) error {
	err := os.RemoveAll(path)
	if err != nil {
		return err
	}
	if err != nil {
		return err
	}

	err = os.MkdirAll(path, 0755)
	if err != nil {
		return err
	}
	return nil
}

// ExtractImage will extract the contents of the image at path to the directory
// at dst. If fileMap is set, only files in it will be extracted.
func ExtractImage(path, dst string, fileMap map[string]struct{}) error {
	dst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	dr, err := aci.NewCompressedReader(file)
	if err != nil {
		return fmt.Errorf("error decompressing image: %v", err)
	}
	defer dr.Close()

	uidRange := user.NewBlankUidRange()

	if os.Geteuid() == 0 {
		return rkttar.ExtractTar(dr, dst, true, uidRange, fileMap)
	}

	editor, err := rkttar.NewUidShiftingFilePermEditor(uidRange)
	if err != nil {
		return fmt.Errorf("error determining current user: %v", err)
	}
	return rkttar.ExtractTarInsecure(tar.NewReader(dr), dst, true, fileMap, editor)
}

func PathWalker(twriter *tar.Writer, tarSrcPath string) func(string, os.FileInfo, error) error {
	prefixLen := len(tarSrcPath + "/")
	return func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == tarSrcPath {
			return nil
		}
		hdrName := path[prefixLen:]

		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			hdr, err := tar.FileInfoHeader(info, target)
			hdr.Name = hdrName
			twriter.WriteHeader(hdr)

		case info.Mode().IsRegular():
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			hdr.Name = hdrName
			twriter.WriteHeader(hdr)

			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()

			n, err := io.Copy(twriter, f)
			if err != nil {
				return err
			}
			if n != info.Size() {
				return fmt.Errorf("underwrite error")
			}
		default:
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			hdr.Name = hdrName
			twriter.WriteHeader(hdr)
		}

		return nil
	}
}
