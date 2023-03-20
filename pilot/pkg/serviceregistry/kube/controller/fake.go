// Copyright Istio Authors
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

package controller

import (
	"sort"
	"strings"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	kubelib "istio.io/istio/pkg/kube"
	filter "istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

const (
	defaultFakeDomainSuffix = "company.com"
)

// FakeXdsUpdater is used to test the registry.
type FakeXdsUpdater struct {
	// Events tracks notifications received by the updater
	Events chan FakeXdsEvent
}

var _ model.XDSUpdater = &FakeXdsUpdater{}

func (fx *FakeXdsUpdater) ConfigUpdate(req *model.PushRequest) {
	names := []string{}
	if req != nil && len(req.ConfigsUpdated) > 0 {
		for key := range req.ConfigsUpdated {
			names = append(names, key.Name)
		}
	}
	sort.Strings(names)
	id := strings.Join(names, ",")
	select {
	case fx.Events <- FakeXdsEvent{Type: "xds", ID: id}:
	default:
	}
}

func (fx *FakeXdsUpdater) ProxyUpdate(_ cluster.ID, _ string) {
	select {
	case fx.Events <- FakeXdsEvent{Type: "proxy"}:
	default:
	}
}

// FakeXdsEvent is used to watch XdsEvents
type FakeXdsEvent struct {
	// Type of the event
	Type string

	// The id of the event
	ID string

	// The endpoints associated with an EDS push if any
	Endpoints []*model.IstioEndpoint
}

// NewFakeXDS creates a XdsUpdater reporting events via a channel.
func NewFakeXDS() *FakeXdsUpdater {
	return &FakeXdsUpdater{
		Events: make(chan FakeXdsEvent, 100),
	}
}

func (fx *FakeXdsUpdater) EDSUpdate(_ model.ShardKey, hostname string, _ string, entry []*model.IstioEndpoint) {
	if len(entry) > 0 {
		select {
		case fx.Events <- FakeXdsEvent{Type: "eds", ID: hostname, Endpoints: entry}:
		default:
		}
	}
}

func (fx *FakeXdsUpdater) EDSCacheUpdate(_ model.ShardKey, hostname, _ string, entry []*model.IstioEndpoint) {
	if len(entry) > 0 {
		select {
		case fx.Events <- FakeXdsEvent{Type: "eds cache", ID: hostname, Endpoints: entry}:
		default:
		}
	}
}

// SvcUpdate is called when a service port mapping definition is updated.
// This interface is WIP - labels, annotations and other changes to service may be
// updated to force a EDS and CDS recomputation and incremental push, as it doesn't affect
// LDS/RDS.
func (fx *FakeXdsUpdater) SvcUpdate(_ model.ShardKey, hostname string, _ string, _ model.Event) {
	select {
	case fx.Events <- FakeXdsEvent{Type: "service", ID: hostname}:
	default:
	}
}

func (fx *FakeXdsUpdater) RemoveShard(shardKey model.ShardKey) {
	select {
	case fx.Events <- FakeXdsEvent{Type: "removeShard", ID: shardKey.String()}:
	default:
	}
}

func (fx *FakeXdsUpdater) WaitOrFail(t test.Failer, et string) *FakeXdsEvent {
	t.Helper()
	for {
		select {
		case e := <-fx.Events:
			if e.Type == et {
				return &e
			}
			log.Infof("skipping event %q want %q", e.Type, et)
			continue
		case <-time.After(time.Second * 5):
			t.Fatalf("timed out waiting for %v", et)
		}
	}
}

// Clear any pending event
func (fx *FakeXdsUpdater) Clear() {
	wait := true
	for wait {
		select {
		case <-fx.Events:
		default:
			wait = false
		}
	}
}

// AssertEmpty ensures there are no events in the channel
func (fx *FakeXdsUpdater) AssertEmpty(t test.Failer, dur time.Duration) {
	if dur == 0 {
		select {
		case e := <-fx.Events:
			t.Fatalf("got unexpected event %+v", e)
		default:
		}
	} else {
		select {
		case e := <-fx.Events:
			t.Fatalf("got unexpected event %+v", e)
		case <-time.After(dur):
		}
	}
}

type FakeControllerOptions struct {
	Client                    kubelib.Client
	NetworksWatcher           mesh.NetworksWatcher
	MeshWatcher               mesh.Watcher
	ServiceHandler            model.ServiceHandler
	Mode                      EndpointMode
	ClusterID                 cluster.ID
	WatchedNamespaces         string
	DomainSuffix              string
	XDSUpdater                model.XDSUpdater
	DiscoveryNamespacesFilter filter.DiscoveryNamespacesFilter
	Stop                      chan struct{}
	SkipRun                   bool
	ConfigController          model.ConfigStoreController
}

type FakeController struct {
	*Controller
}

func NewFakeControllerWithOptions(t test.Failer, opts FakeControllerOptions) (*FakeController, *FakeXdsUpdater) {
	xdsUpdater := opts.XDSUpdater
	if xdsUpdater == nil {
		xdsUpdater = NewFakeXDS()
	}

	domainSuffix := defaultFakeDomainSuffix
	if opts.DomainSuffix != "" {
		domainSuffix = opts.DomainSuffix
	}
	if opts.Client == nil {
		opts.Client = kubelib.NewFakeClient()
	}
	if opts.MeshWatcher == nil {
		opts.MeshWatcher = mesh.NewFixedWatcher(&meshconfig.MeshConfig{})
	}

	meshServiceController := aggregate.NewController(aggregate.Options{MeshHolder: opts.MeshWatcher})

	options := Options{
		DomainSuffix:              domainSuffix,
		XDSUpdater:                xdsUpdater,
		Metrics:                   &model.Environment{},
		NetworksWatcher:           opts.NetworksWatcher,
		MeshWatcher:               opts.MeshWatcher,
		EndpointMode:              opts.Mode,
		ClusterID:                 opts.ClusterID,
		DiscoveryNamespacesFilter: opts.DiscoveryNamespacesFilter,
		MeshServiceController:     meshServiceController,
		ConfigController:          opts.ConfigController,
	}
	c := NewController(opts.Client, options)
	meshServiceController.AddRegistry(c)

	if opts.ServiceHandler != nil {
		c.AppendServiceHandler(opts.ServiceHandler)
	}

	t.Cleanup(func() {
		c.client.Shutdown()
	})
	if !opts.SkipRun {
		t.Cleanup(func() {
			assert.NoError(t, queue.WaitForClose(c.queue, time.Second*5))
		})
	}
	c.stop = opts.Stop
	if c.stop == nil {
		// If we created the stop, clean it up. Otherwise, caller is responsible
		c.stop = test.NewStop(t)
	}
	opts.Client.RunAndWait(c.stop)
	var fx *FakeXdsUpdater
	if x, ok := xdsUpdater.(*FakeXdsUpdater); ok {
		fx = x
	}

	if !opts.SkipRun {
		go c.Run(c.stop)
		kubelib.WaitForCacheSync(c.stop, c.HasSynced)
	}

	return &FakeController{c}, fx
}
