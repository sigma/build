// Copyright 2017 The acbuild Authors
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

package lib

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/containers/build/lib/oci"
	"github.com/containers/build/util"
	digest "github.com/opencontainers/go-digest"
)

func (a *ACBuild) MarkLayerDirty(layerDir string) (err error) {
	if strings.HasSuffix(layerDir, "new-layer") {
		return nil
	}
	return ioutil.WriteFile(layerDir+".dirty", nil, 0644)
}

func (a *ACBuild) RehashTopLayer() (err error) {
	if a.Mode != BuildModeOCI {
		return fmt.Errorf("only OCIs need to rehash the top layer")
	}

	var topLayerID digest.Digest
	switch ociMan := a.man.(type) {
	case *oci.Image:
		topLayerID = ociMan.GetTopLayerDigest()
	default:
		return fmt.Errorf("internal error: mismatched manifest type and build mode???")
	}

	var markerPath string
	if topLayerID != "" {
		markerPath = path.Join(a.OCIExpandedBlobsPath, topLayerID.Algorithm().String(), topLayerID.Hex()+".dirty")
		_, err := os.Stat(markerPath)
		if err != nil && os.IsNotExist(err) {
			// Dirty marker does not exist
			return nil
		}
		// Dirty marker exists, or other error -> do the rehash
	}

	currentLayer, err := a.expandTopOCILayer()
	if err != nil {
		return err
	}
	err = a.rehashAndStoreOCIBlob(currentLayer, false)
	if err != nil {
		return err
	}
	err = os.Remove(markerPath)
	if err != nil {
		return fmt.Errorf("could not remove dirty marker: %v", err)
	}
	return nil
}

func (a *ACBuild) NewLayer() (err error) {
	if err = a.lock(); err != nil {
		return err
	}
	defer func() {
		if err1 := a.unlock(); err == nil {
			err = err1
		}
	}()

	if a.Mode != BuildModeOCI {
		return fmt.Errorf("adding layers is currently only supported in OCI builds")
	}

	a.RehashTopLayer()

	newLayer, err := util.OCINewExpandedLayer(a.OCIExpandedBlobsPath)
	if err != nil {
		return err
	}
	return a.rehashAndStoreOCIBlob(newLayer, true)
}
