/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package source

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	kubeinformers "k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/costinm/dns-sync/endpoint"
)

type nodeSource struct {
	client           kubernetes.Interface
	fqdnTemplate     string
	nodeInformer     coreinformers.NodeInformer
	labelSelector    labels.Selector
}

// NewNodeSource creates a new nodeSource with the given config.
func NewNodeSource(ctx context.Context, kubeClient kubernetes.Interface, fqdnTemplate string, labelSelector labels.Selector) (endpoint.Source, error) {

	// Use shared informers to listen for add/update/delete of nodes.
	// Set resync period to 0, to prevent processing when nothing has changed
	informerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 0)
	nodeInformer := informerFactory.Core().V1().Nodes()

	// Add default resource event handler to properly initialize informer.
	nodeInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				log.Debug("node added")
			},
		},
	)

	informerFactory.Start(ctx.Done())

	// wait for the local cache to be populated.
	if err := WaitForCacheSync(context.Background(), informerFactory); err != nil {
		return nil, err
	}

	return &nodeSource{
		client:           kubeClient,
		fqdnTemplate:     fqdnTemplate,
		nodeInformer:     nodeInformer,
		labelSelector:    labelSelector,
	}, nil
}

// Endpoints returns endpoint objects for each service that should be processed.
func (ns *nodeSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	nodes, err := ns.nodeInformer.Lister().List(ns.labelSelector)
	if err != nil {
		return nil, err
	}

	endpoints := map[endpoint.EndpointKey]*endpoint.Endpoint{}

	// create endpoints for all nodes
	for _, node := range nodes {
		// Check controller annotation to see if we are responsible.
		// Removed: this source syncs all nodes (label selector allowed)
		//controller, ok := node.Annotations[controllerAnnotationKey]
		//if ok && controller != controllerAnnotationValue {
		//	log.Debugf("Skipping node %s because controller value does not match, found: %s, required: %s",
		//		node.Name, controller, controllerAnnotationValue)
		//	continue
		//}
		//
		log.Debugf("creating endpoint for node %s", node.Name)

		ttl := GetTTLFromAnnotations(node.Annotations, fmt.Sprintf("node/%s", node.Name))

		// create new endpoint with the information we already have
		ep := &endpoint.Endpoint{
			RecordTTL: ttl,
		}

		ep.DNSName = node.Name + ns.fqdnTemplate

		addrs, err := ns.nodeAddresses(node)
		if err != nil {
			return nil, fmt.Errorf("failed to get node address from %s: %w", node.Name, err)
		}

		ep.Labels = endpoint.NewLabels()
		for _, addr := range addrs {
			log.Debugf("adding endpoint %s target %s", ep, addr)
			key := endpoint.EndpointKey{
				DNSName:    ep.DNSName,
				RecordType: SuitableType(addr),
			}
			if _, ok := endpoints[key]; !ok {
				epCopy := *ep
				epCopy.RecordType = key.RecordType
				endpoints[key] = &epCopy
			}
			endpoints[key].Targets = append(endpoints[key].Targets, addr)
		}
	}

	endpointsSlice := []*endpoint.Endpoint{}
	for _, ep := range endpoints {
		endpointsSlice = append(endpointsSlice, ep)
	}

	return endpointsSlice, nil
}

func (ns *nodeSource) AddEventHandler(ctx context.Context, handler func()) {
}

// nodeAddress returns node's externalIP and if that's not found, node's internalIP
// basically what k8s.io/kubernetes/pkg/util/node.GetPreferredNodeAddress does
func (ns *nodeSource) nodeAddresses(node *v1.Node) ([]string, error) {
	addresses := map[v1.NodeAddressType][]string{
		v1.NodeExternalIP: {},
		v1.NodeInternalIP: {},
	}
	var ipv6Addresses []string

	for _, addr := range node.Status.Addresses {
		addresses[addr.Type] = append(addresses[addr.Type], addr.Address)
		// IPv6 addresses are labeled as NodeInternalIP despite being usable externally as well.
		if addr.Type == v1.NodeInternalIP && SuitableType(addr.Address) == endpoint.RecordTypeAAAA {
			ipv6Addresses = append(ipv6Addresses, addr.Address)
		}
	}

	if len(addresses[v1.NodeExternalIP]) > 0 {
		return append(addresses[v1.NodeExternalIP], ipv6Addresses...), nil
	}

	if len(addresses[v1.NodeInternalIP]) > 0 {
		return addresses[v1.NodeInternalIP], nil
	}

	return nil, fmt.Errorf("could not find node address for %s", node.Name)
}

