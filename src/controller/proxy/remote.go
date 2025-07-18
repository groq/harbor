// Copyright Project Harbor Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package proxy

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/docker/distribution"

	"github.com/goharbor/harbor/src/lib"
	"github.com/goharbor/harbor/src/lib/metric"
	"github.com/goharbor/harbor/src/pkg/reg"
	"github.com/goharbor/harbor/src/pkg/reg/adapter"
	"github.com/goharbor/harbor/src/pkg/reg/model"
)

// RemoteInterface defines operations related to remote repository under proxy
type RemoteInterface interface {
	// BlobReader create a reader for remote blob
	BlobReader(repo, dig string) (int64, io.ReadCloser, error)
	// Manifest get manifest by reference
	Manifest(repo string, ref string) (distribution.Manifest, string, error)
	// ManifestExist checks manifest exist, if exist, return digest
	ManifestExist(repo string, ref string) (bool, *distribution.Descriptor, error)
	// ListTags returns all tags of the repo
	ListTags(repo string) ([]string, error)
}

// remoteHelper defines operations related to remote repository under proxy
type remoteHelper struct {
	regID        int64
	registryName string
	registry     adapter.ArtifactRegistry
	registryMgr  reg.Manager
	opts         *Options
}

// NewRemoteHelper create a remote interface
func NewRemoteHelper(ctx context.Context, regID int64, mgr reg.Manager, opts ...Option) (RemoteInterface, error) {
	r := &remoteHelper{
		regID:       regID,
		registryMgr: mgr,
		opts:        NewOptions(opts...),
	}
	if err := r.init(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *remoteHelper) init(ctx context.Context) error {
	if r.registry != nil {
		return nil
	}
	reg, err := r.registryMgr.Get(ctx, r.regID)
	if err != nil {
		return err
	}
	if reg == nil {
		return fmt.Errorf("failed to get registry, registryID: %v", r.regID)
	}
	if reg.Status != model.Healthy {
		return fmt.Errorf("current registry is unhealthy, regID:%v, Name:%v, Status: %v", reg.ID, reg.Name, reg.Status)
	}
	factory, err := adapter.GetFactory(reg.Type)
	if err != nil {
		return err
	}
	adp, err := factory.Create(reg)
	if err != nil {
		return err
	}
	r.registryName = reg.Name
	r.registry = adp.(adapter.ArtifactRegistry)
	return nil
}

func (r *remoteHelper) BlobReader(repo, dig string) (size int64, bReader io.ReadCloser, err error) {
	defer func() {
		recordRemoteRequest("blob", r.registryName, err == nil)
	}()
	sz, bReader, err := r.registry.PullBlob(repo, dig)
	if err != nil {
		return
	}
	if r.opts != nil && r.opts.Speed > 0 {
		bReader = lib.NewReader(bReader, r.opts.Speed)
	}
	return sz, bReader, err
}

func (r *remoteHelper) Manifest(repo string, ref string) (manifest distribution.Manifest, digest string, err error) {
	defer func() {
		recordRemoteRequest("manifest", r.registryName, err == nil)
	}()
	return r.registry.PullManifest(repo, ref)
}

func (r *remoteHelper) ManifestExist(repo string, ref string) (exists bool, descriptor *distribution.Descriptor, err error) {
	defer func() {
		recordRemoteRequest("manifest_exist", r.registryName, err == nil)
	}()
	return r.registry.ManifestExist(repo, ref)
}

func (r *remoteHelper) ListTags(repo string) (tags []string, err error) {
	defer func() {
		recordRemoteRequest("list_tags", r.registryName, err == nil)
	}()
	return r.registry.ListTags(repo)
}

// recordRemoteRequest increments the remote request counter
func recordRemoteRequest(operation, registryName string, success bool) {
	metric.TotalRemoteRegistryReqCnt.WithLabelValues(operation, registryName, strconv.FormatBool(success)).Inc()
}
