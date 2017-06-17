// Copyright 2016 The acbuild Authors
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
	"os"
	"path"

	digest "github.com/opencontainers/go-digest"
)

func OCIExtractLayers(layerIDs []digest.Digest, imageLoc, blobsDest string) error {
	for _, layerID := range layerIDs {
		algo, hash := layerID.Algorithm().String(), layerID.Hex()

		from := path.Join(imageLoc, "blobs", algo, hash)
		to := path.Join(blobsDest, algo, hash)

		_, err := os.Stat(to)
		if err == nil {
			// This has already been extracted
			break
		}

		err = os.MkdirAll(to, 0755)
		if err != nil {
			return err
		}

		err = ExtractImage(from, to, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func OCINewExpandedLayer(ociExpandedBlobsPath string) (string, error) {
	targetPath := path.Join(ociExpandedBlobsPath, "sha256", "new-layer")
	_, err := os.Stat(targetPath)
	if os.IsNotExist(err) {
		os.RemoveAll(targetPath)
	}
	err = os.MkdirAll(targetPath, 0755)
	if err != nil {
		return "", err
	}
	return targetPath, nil
}
