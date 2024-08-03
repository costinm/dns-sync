/*
Copyright 2021 The Kubernetes Authors.

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
	"sort"
	"strings"
	"text/template"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	cache "k8s.io/client-go/tools/cache"
	v1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	gateway "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
	informers "sigs.k8s.io/gateway-api/pkg/client/informers/externalversions"
	informers_v1 "sigs.k8s.io/gateway-api/pkg/client/informers/externalversions/apis/v1beta1"

	"github.com/costinm/dns-sync/endpoint"
)

const (
	gatewayGroup = "gateway.networking.k8s.io"
	gatewayKind  = "Gateway"
)

type gatewayRoute interface {
	// Object returns the underlying route object to be used by templates.
	Object() kubeObject
	// Metadata returns the route's metadata.
	Metadata() *metav1.ObjectMeta
	// Hostnames returns the route's specified hostnames.
	Hostnames() []v1.Hostname
	// Protocol returns the route's protocol type.
	Protocol() v1.ProtocolType
	// RouteStatus returns the route's common status.
	RouteStatus() v1.RouteStatus
}

type newGatewayRouteInformerFunc func(informers.SharedInformerFactory) gatewayRouteInformer

type gatewayRouteInformer interface {
	List(namespace string, selector labels.Selector) ([]gatewayRoute, error)
	Informer() cache.SharedIndexInformer
}

func newGatewayInformerFactory(client gateway.Interface, namespace string, labelSelector labels.Selector) informers.SharedInformerFactory {
	var opts []informers.SharedInformerOption
	if namespace != "" {
		opts = append(opts, informers.WithNamespace(namespace))
	}
	if labelSelector != nil && !labelSelector.Empty() {
		lbls := labelSelector.String()
		opts = append(opts, informers.WithTweakListOptions(func(o *metav1.ListOptions) {
			o.LabelSelector = lbls
		}))
	}
	return informers.NewSharedInformerFactoryWithOptions(client, 0, opts...)
}

// gatewayRouteSource extracts DNS entries from K8S routes or Gateways
type gatewayRouteSource struct {
	gwNamespace string
	gwLabels    labels.Selector
	gwInformer  informers_v1.GatewayInformer

	rtKind        string
	rtNamespace   string
	rtLabels      labels.Selector
	rtAnnotations labels.Selector

	// rtInformer is the informer for the routes. Will be nil if the source is directly using Gateway
	rtInformer gatewayRouteInformer

	nsInformer coreinformers.NamespaceInformer

	fqdnTemplate             *template.Template
	combineFQDNAnnotation    bool
	ignoreHostnameAnnotation bool
}

// NewGatewaySource creates a new Gateway source with the given config.
func NewGatewaySource(clients ClientGenerator, config *Config) (endpoint.Source, error) {
	return newGatewayRouteSource(clients, config, "Gateway", nil)
}

func newGatewayRouteSource(clients ClientGenerator, config *Config, kind string, newInformerFn newGatewayRouteInformerFunc) (endpoint.Source, error) {
	ctx := context.TODO()

	gwLabels, err := getLabelSelector(config.GatewayLabelFilter)
	if err != nil {
		return nil, err
	}
	rtLabels := config.LabelFilter
	if rtLabels == nil {
		rtLabels = labels.Everything()
	}
	rtAnnotations, err := getLabelSelector(config.AnnotationFilter)
	if err != nil {
		return nil, err
	}
	tmpl, err := parseTemplate(config.FQDNTemplate)
	if err != nil {
		return nil, err
	}

	client, err := clients.GatewayClient()
	if err != nil {
		return nil, err
	}

	informerFactory := newGatewayInformerFactory(client, config.GatewayNamespace, gwLabels)

	gwInformer := informerFactory.Gateway().V1beta1().Gateways() // TODO: Gateway informer should be shared across gateway sources.
	gwInformer.Informer()                                        // Register with factory before starting.

	rtInformerFactory := informerFactory
	if config.Namespace != config.GatewayNamespace || !selectorsEqual(rtLabels, gwLabels) {
		rtInformerFactory = newGatewayInformerFactory(client, config.Namespace, rtLabels)
	}
	var rtInformer gatewayRouteInformer
	if newInformerFn != nil {
		rtInformer = newInformerFn(rtInformerFactory)
		rtInformer.Informer() // Register with factory before starting.
	}
	kubeClient, err := clients.KubeClient()
	if err != nil {
		return nil, err
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	nsInformer := kubeInformerFactory.Core().V1().Namespaces() // TODO: Namespace informer should be shared across gateway sources.
	nsInformer.Informer()                                      // Register with factory before starting.

	informerFactory.Start(wait.NeverStop)
	kubeInformerFactory.Start(wait.NeverStop)

	if rtInformerFactory != informerFactory {
		rtInformerFactory.Start(wait.NeverStop)

		if err := WaitForCacheSync(ctx, rtInformerFactory); err != nil {
			return nil, err
		}
	}
	// TODO: seems to timeout...
	if err := WaitForCacheSync(ctx, informerFactory); err != nil {
		return nil, err
	}
	if err := WaitForCacheSync(ctx, kubeInformerFactory); err != nil {
		return nil, err
	}

	src := &gatewayRouteSource{
		gwNamespace: config.GatewayNamespace,
		gwLabels:    gwLabels,
		gwInformer:  gwInformer,

		rtKind:        kind,
		rtNamespace:   config.Namespace,
		rtLabels:      rtLabels,
		rtAnnotations: rtAnnotations,
		rtInformer:    rtInformer,

		nsInformer: nsInformer,

		fqdnTemplate:             tmpl,
		combineFQDNAnnotation:    config.CombineFQDNAndAnnotation,
		ignoreHostnameAnnotation: config.IgnoreHostnameAnnotation,
	}
	return src, nil
}

func (src *gatewayRouteSource) AddEventHandler(ctx context.Context, handler func()) {
	log.Debugf("Adding event handlers for %s", src.rtKind)
	eventHandler := EventHandlerFunc(handler)
	src.gwInformer.Informer().AddEventHandler(eventHandler)
	if src.rtInformer != nil {
		src.rtInformer.Informer().AddEventHandler(eventHandler)
	}
	src.nsInformer.Informer().AddEventHandler(eventHandler)
}

func (src *gatewayRouteSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	var endpoints []*endpoint.Endpoint

	if src.rtInformer == nil {
		gateways, err := src.gwInformer.Lister().Gateways(src.gwNamespace).List(src.gwLabels)
		if err != nil {
			return nil, err
		}
		namespaces, err := src.nsInformer.Lister().List(labels.Everything())
		if err != nil {
			return nil, err
		}
		kind := strings.ToLower(src.rtKind)
		resolver := newGatewayRouteResolver(src, gateways, namespaces)

		for _, gw := range gateways {
			// Filter by annotations.
			meta := gw

			// Get Route hostnames and their targets.
			hostTargets, err := resolver.resolveGateway(gw)
			if err != nil {
				return nil, err
			}
			if len(hostTargets) == 0 {
				log.Debugf("No endpoints could be generated from %s %s/%s", src.rtKind, meta.Namespace, meta.Name)
				continue
			}

			// Create endpoints from hostnames and targets.
			resource := fmt.Sprintf("%s/%s/%s", kind, meta.Namespace, meta.Name)
			for host, targets := range hostTargets {
				endpoints = append(endpoints, EndpointsForHostname(host, targets, endpoint.TTL(0), nil, "", resource)...)
			}
			log.Debugf("Endpoints generated from %s %s/%s: %v", src.rtKind, meta.Namespace, meta.Name, endpoints)
		}
		return endpoints, nil
	}

	return endpoints, nil
}

func namespacedName(namespace, name string) types.NamespacedName {
	return types.NamespacedName{Namespace: namespace, Name: name}
}

type gatewayRouteResolver struct {
	src *gatewayRouteSource
	gws map[types.NamespacedName]gatewayListeners
	nss map[string]*corev1.Namespace
}

type gatewayListeners struct {
	gateway   *v1.Gateway
	listeners map[v1.SectionName][]v1.Listener
}

func newGatewayRouteResolver(src *gatewayRouteSource, gateways []*v1.Gateway, namespaces []*corev1.Namespace) *gatewayRouteResolver {
	// Create Gateway Listener lookup table.
	gws := make(map[types.NamespacedName]gatewayListeners, len(gateways))
	for _, gw := range gateways {
		lss := make(map[v1.SectionName][]v1.Listener, len(gw.Spec.Listeners)+1)
		for i, lis := range gw.Spec.Listeners {
			lss[lis.Name] = gw.Spec.Listeners[i : i+1]
		}
		lss[""] = gw.Spec.Listeners
		gws[namespacedName(gw.Namespace, gw.Name)] = gatewayListeners{
			gateway:   gw,
			listeners: lss,
		}
	}
	// Create Namespace lookup table.
	nss := make(map[string]*corev1.Namespace, len(namespaces))
	for _, ns := range namespaces {
		nss[ns.Name] = ns
	}
	return &gatewayRouteResolver{
		src: src,
		gws: gws,
		nss: nss,
	}
}

func (c *gatewayRouteResolver) resolveGateway(gw *v1.Gateway) (map[string]endpoint.Targets, error) {
	hostTargets := make(map[string]endpoint.Targets)

	listeners := gw.Spec.Listeners
	for _, lis := range listeners {
		if lis.Hostname == nil {
			continue
		}
		host := string(*lis.Hostname)
		if host == "" {
			continue
		}

		if len(gw.Spec.Addresses) > 0 {
			for _, addr := range gw.Spec.Addresses {
				hostTargets[host] = append(hostTargets[host], addr.Value)
			}
		} else {
			for _, addr := range gw.Status.Addresses {
				hostTargets[host] = append(hostTargets[host], addr.Value)
			}
		}
	}
	// If a Gateway has multiple matching Listeners for the same host, then we'll
	// add its IPs to the target list multiple times and should dedupe them.
	for host, targets := range hostTargets {
		hostTargets[host] = uniqueTargets(targets)
	}
	return hostTargets, nil
}


func uniqueTargets(targets endpoint.Targets) endpoint.Targets {
	if len(targets) < 2 {
		return targets
	}
	sort.Strings([]string(targets))
	prev := targets[0]
	n := 1
	for _, v := range targets[1:] {
		if v == prev {
			continue
		}
		prev = v
		targets[n] = v
		n++
	}
	return targets[:n]
}

func selectorsEqual(a, b labels.Selector) bool {
	if a == nil || b == nil {
		return a == b
	}
	aReq, aOK := a.DeepCopySelector().Requirements()
	bReq, bOK := b.DeepCopySelector().Requirements()
	if aOK != bOK || len(aReq) != len(bReq) {
		return false
	}
	sort.Stable(labels.ByKey(aReq))
	sort.Stable(labels.ByKey(bReq))
	for i, r := range aReq {
		if !r.Equal(bReq[i]) {
			return false
		}
	}
	return true
}
