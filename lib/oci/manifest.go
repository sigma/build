// Copyright 2016 The appc Authors
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

package oci

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"runtime"

	"github.com/containers/build/util"

	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ociImage "github.com/opencontainers/image-spec/specs-go/v1"
)

var (
	// ErrNotFound is returned when acbuild is asked to remove an element from a
	// list and the element is not present in the list
	ErrNotFound = fmt.Errorf("element to be removed does not exist in this image")
)

const OCISchemaVersion = 2

// TODO(lda): This is in newer versions of image-spec/specs-go
const AnnotationRefName = "org.opencontainers.image.ref.name"

// Manifest is a struct with an open handle to a manifest that it can manipulate
type Image struct {
	ociPath  string
	refName  string
	config   ociImage.Image
	manifest ociImage.Manifest
	manDesc  ociImage.Descriptor
}

func LoadImage(ociPath string) (*Image, error) {
	i := &Image{
		ociPath: ociPath,
	}

	blobDir := path.Join(ociPath, "blobs")

	indexFile, err := os.OpenFile(path.Join(ociPath, "index.json"), os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	defer indexFile.Close()
	indexBlob, err := ioutil.ReadAll(indexFile)
	if err != nil {
		return nil, err
	}
	var index ociImage.Index
	err = json.Unmarshal(indexBlob, &index)
	if err != nil {
		return nil, err
	}

	// Look for refs, pick the first one we find
	for _, manifest := range index.Manifests {
		if manifest.MediaType == ociImage.MediaTypeImageManifest {
			i.manDesc = manifest
			if manifest.Annotations != nil && manifest.Annotations[AnnotationRefName] != "" {
				i.refName = manifest.Annotations[AnnotationRefName]
			}
			break
		}
	}
	if len(i.manDesc.Digest) == 0 {
		return nil, fmt.Errorf("no manifests found in image")
	}

	// Open the manifest, read it, unmarshal it, and parse the config's hash
	manDigest := &i.manDesc.Digest
	manifestFile, err := os.OpenFile(path.Join(blobDir, manDigest.Algorithm().String(), manDigest.Hex()), os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	defer manifestFile.Close()
	manifestBlob, err := ioutil.ReadAll(manifestFile)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(manifestBlob, &i.manifest)
	if err != nil {
		return nil, err
	}
	configHash := i.manifest.Config.Digest

	// Open the config, read it, unmarshal it
	configFile, err := os.OpenFile(path.Join(blobDir, configHash.Algorithm().String(), configHash.Hex()), os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	defer configFile.Close()
	configBlob, err := ioutil.ReadAll(configFile)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(configBlob, &i.config)
	if err != nil {
		return nil, err
	}

	return i, nil
}

func (i *Image) save() error {
	// Remove the old config
	oldConfigHash := i.manifest.Config.Digest
	err := os.Remove(path.Join(i.ociPath, "blobs", oldConfigHash.Algorithm().String(), oldConfigHash.Hex()))
	if err != nil {
		return err
	}
	// Save the new config
	configHashAlgo, configHash, configSize, err := util.MarshalHashAndWrite(i.ociPath, i.config)
	if err != nil {
		return err
	}
	i.manifest.Config.Digest = digest.NewDigestFromHex(configHashAlgo, configHash)
	i.manifest.Config.Size = int64(configSize)

	// Remove the old manifest
	oldManifestHash := i.manDesc.Digest
	err = os.Remove(path.Join(i.ociPath, "blobs", oldManifestHash.Algorithm().String(), oldManifestHash.Hex()))
	if err != nil {
		return err
	}
	// Save the new manifest
	manifestHashAlgo, manifestHash, manifestSize, err := util.MarshalHashAndWrite(i.ociPath, i.manifest)
	if err != nil {
		return err
	}
	i.manDesc.Digest = digest.NewDigestFromHex(manifestHashAlgo, manifestHash)
	i.manDesc.Size = int64(manifestSize)

	// Update the index
	var idxManAnnotations map[string]string
	if i.refName != "" {
		idxManAnnotations = map[string]string{AnnotationRefName: i.refName}
	}

	index := ociImage.Index{
		Versioned: specs.Versioned{
			SchemaVersion: OCISchemaVersion,
		},
		Manifests: []ociImage.Descriptor{
			{
				MediaType:   ociImage.MediaTypeImageManifest,
				Digest:      i.manDesc.Digest,
				Size:        int64(i.manDesc.Size),
				Annotations: idxManAnnotations,
				Platform: &ociImage.Platform{
					Architecture: runtime.GOARCH,
					OS:           runtime.GOOS,
				},
			},
		},
	}
	indexBlob, err := json.Marshal(index)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(path.Join(i.ociPath, "index.json"), indexBlob, 0644)
	if err != nil {
		return err
	}

	return nil
}

func (i *Image) GetConfig() ociImage.Image {
	return i.config
}

func (i *Image) GetManifest() ociImage.Manifest {
	return i.manifest
}

func (i *Image) GetRefName() string {
	return i.refName
}

func (i *Image) GetDiffIDs() []digest.Digest {
	return i.config.RootFS.DiffIDs
}

func (i *Image) GetLayerDigests() []digest.Digest {
	numLayers := len(i.manifest.Layers)
	layerDigests := make([]digest.Digest, numLayers, numLayers)
	for index, layer := range i.manifest.Layers {
		layerDigests[index] = layer.Digest
	}
	return layerDigests
}

func (i *Image) Print(w io.Writer, prettyPrint, printConfig bool) error {
	var configblob []byte
	var err error
	var toPrint interface{}
	if printConfig {
		toPrint = i.config
	} else {
		toPrint = i.manifest
	}
	if prettyPrint {
		configblob, err = json.MarshalIndent(toPrint, "", "    ")
	} else {
		configblob, err = json.Marshal(toPrint)
	}
	if err != nil {
		return err
	}
	configblob = append(configblob, '\n')
	n, err := w.Write(configblob)
	if err != nil {
		return err
	}
	if n < len(configblob) {
		return fmt.Errorf("short write")
	}
	return nil
}

func (i *Image) UpdateTopLayer(layerDigest, diffId digest.Digest, size int64) (digest.Digest, error) {
	var oldLayerDigest digest.Digest
	if len(i.config.RootFS.DiffIDs) == 0 {
		i.config.RootFS = ociImage.RootFS{
			Type:    "layers",
			DiffIDs: []digest.Digest{diffId},
		}
	} else {
		i.config.RootFS.DiffIDs[len(i.config.RootFS.DiffIDs)-1] = diffId
	}

	layerDescriptor := ociImage.Descriptor{
		MediaType: ociImage.MediaTypeImageLayerGzip,
		Digest:    layerDigest,
		Size:      size,
	}

	if len(i.manifest.Layers) == 0 {
		i.manifest.Layers = []ociImage.Descriptor{layerDescriptor}
	} else {
		numLayers := len(i.manifest.Layers)
		oldLayerDigest = i.manifest.Layers[numLayers-1].Digest
		i.manifest.Layers[numLayers-1] = layerDescriptor
	}

	return oldLayerDigest, i.save()
}

func (i *Image) NewTopLayer(layerDigest, diffId digest.Digest, size int64) error {
	if len(i.config.RootFS.DiffIDs) == 0 {
		i.config.RootFS = ociImage.RootFS{
			Type:    "layers",
			DiffIDs: []digest.Digest{diffId},
		}
	} else {
		i.config.RootFS.DiffIDs = append(i.config.RootFS.DiffIDs, diffId)
	}

	layerDescriptor :=
		ociImage.Descriptor{
			MediaType: ociImage.MediaTypeImageLayerGzip,
			Digest:    layerDigest,
			Size:      size,
		}
	if len(i.manifest.Layers) == 0 {
		i.manifest.Layers = []ociImage.Descriptor{layerDescriptor}
	} else {
		i.manifest.Layers = append(i.manifest.Layers, layerDescriptor)
	}

	return i.save()
}
