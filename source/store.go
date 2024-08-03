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
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/costinm/dns-sync/endpoint"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	gateway "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

// ErrSourceNotFound is returned when a requested source doesn't exist.
var ErrSourceNotFound = errors.New("source not found")

// Config holds shared configuration options for all Sources.
type Config struct {
	endpoint.SourceSpec `json:",inline"`

	AnnotationFilter               string

	LabelFilter                    labels.Selector
	IngressClassNames              []string
	FQDNTemplate                   string
	CombineFQDNAndAnnotation       bool
	IgnoreHostnameAnnotation       bool
	IgnoreIngressTLSSpec           bool
	IgnoreIngressRulesSpec         bool
	GatewayNamespace               string
	GatewayLabelFilter             string
	Compatibility                  string
	PublishInternal                bool
	PublishHostIP                  bool
	AlwaysPublishNotReadyAddresses bool
	ConnectorServer                string
	CRDSourceAPIVersion            string
	CRDSourceKind                  string
	KubeConfig                     string
	APIServerURL                   string
	ServiceTypeFilter              []string
	CFAPIEndpoint                  string
	CFUsername                     string
	CFPassword                     string
	GlooNamespaces                 []string
	SkipperRouteGroupVersion       string
	RequestTimeout                 time.Duration
	DefaultTargets                 []string
	OCPRouterName                  string
	UpdateEvents                   bool
	ResolveLoadBalancerHostname    bool
	TraefikDisableLegacy           bool
	TraefikDisableNew              bool
}

// ClientGenerator provides clients
type ClientGenerator interface {
	KubeClient() (kubernetes.Interface, error)
	GatewayClient() (gateway.Interface, error)
	IstioClient() (istioclient.Interface, error)
	//CloudFoundryClient(cfAPPEndpoint string, cfUsername string, cfPassword string) (*cfclient.Client, error)
	DynamicKubernetesClient() (dynamic.Interface, error)
}

// SingletonClientGenerator stores provider clients and guarantees that only one instance of client
// will be generated
type SingletonClientGenerator struct {
	KubeConfig      string
	APIServerURL    string
	RequestTimeout  time.Duration
	kubeClient      kubernetes.Interface
	gatewayClient   gateway.Interface
	istioClient     *istioclient.Clientset
	dynKubeClient   dynamic.Interface

	kubeOnce        sync.Once
	gatewayOnce     sync.Once
	istioOnce       sync.Once
	dynCliOnce      sync.Once
}

// KubeClient generates a kube client if it was not created before
func (p *SingletonClientGenerator) KubeClient() (kubernetes.Interface, error) {
	var err error
	p.kubeOnce.Do(func() {
		p.kubeClient, err = NewKubeClient(p.KubeConfig, p.APIServerURL, p.RequestTimeout)
	})
	return p.kubeClient, err
}

// GatewayClient generates a gateway client if it was not created before
func (p *SingletonClientGenerator) GatewayClient() (gateway.Interface, error) {
	var err error
	p.gatewayOnce.Do(func() {
		p.gatewayClient, err = newGatewayClient(p.KubeConfig, p.APIServerURL, p.RequestTimeout)
	})
	return p.gatewayClient, err
}

func newGatewayClient(kubeConfig, apiServerURL string, requestTimeout time.Duration) (gateway.Interface, error) {
	config, err := instrumentedRESTConfig(kubeConfig, apiServerURL, requestTimeout)
	if err != nil {
		return nil, err
	}
	client, err := gateway.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	log.Infof("Created GatewayAPI client %s", config.Host)
	return client, nil
}

// IstioClient generates an istio go client if it was not created before
func (p *SingletonClientGenerator) IstioClient() (istioclient.Interface, error) {
	var err error
	p.istioOnce.Do(func() {
		p.istioClient, err = NewIstioClient(p.KubeConfig, p.APIServerURL)
	})
	return p.istioClient, err
}

// DynamicKubernetesClient generates a dynamic client if it was not created before
func (p *SingletonClientGenerator) DynamicKubernetesClient() (dynamic.Interface, error) {
	var err error
	p.dynCliOnce.Do(func() {
		p.dynKubeClient, err = NewDynamicKubernetesClient(p.KubeConfig, p.APIServerURL, p.RequestTimeout)
	})
	return p.dynKubeClient, err
}

var SourceFn = map[string]func(ctx context.Context, source string, p ClientGenerator, srcCfg *endpoint.SourceSpec, config *endpoint.SyncConfig) (endpoint.Source, error){}

// BuildWithConfig allows to generate a Source implementation from the shared config
func BuildWithConfig(ctx context.Context, source string, p ClientGenerator, cfg *Config, srcCfg *endpoint.SourceSpec, config *endpoint.SyncConfig) (endpoint.Source, error) {

	// TODO: get a shared informer factory for all sources.

	switch source {
	case "node":
		// Changes: no annotation key, all nodes, suffix instead of template
		client, err := p.KubeClient()
		if err != nil {
			return nil, err
		}
		return NewNodeSource(ctx, client, srcCfg.Suffix, cfg.LabelFilter)
	case "ingress":
		client, err := p.KubeClient()
		if err != nil {
			return nil, err
		}
		return NewIngressSource(ctx, client, cfg.Namespace, cfg.AnnotationFilter, cfg.FQDNTemplate, cfg.CombineFQDNAndAnnotation, cfg.IgnoreHostnameAnnotation, cfg.IgnoreIngressTLSSpec, cfg.IgnoreIngressRulesSpec, cfg.LabelFilter, cfg.IngressClassNames)
	case "pod":
		client, err := p.KubeClient()
		if err != nil {
			return nil, err
		}
		return NewPodSource(ctx, client, cfg.Namespace, cfg.Compatibility)
	case "k8s":
		return NewK8SSource(p, cfg)
	case "gateway":
		return NewGatewaySource(p, cfg)
	case "istio-se":
		kubernetesClient, err := p.KubeClient()
		if err != nil {
			return nil, err
		}
		istioClient, err := p.IstioClient()
		if err != nil {
			return nil, err
		}
		return NewIstioServiceEntrySourceConfig(ctx, kubernetesClient, istioClient,
			ServiceEntrySourceConfig{
				MeshExternalNamespace: "",
				MeshInternalDomain:    "",
				EgressGatewayVIP:      nil,
				HttpVIP:               "",
				UpdateServiceEntry:    false,
			})
	case "istio-gateway":
		kubernetesClient, err := p.KubeClient()
		if err != nil {
			return nil, err
		}
		istioClient, err := p.IstioClient()
		if err != nil {
			return nil, err
		}
		return NewIstioGatewaySource(ctx, kubernetesClient, istioClient, cfg.Namespace, cfg.AnnotationFilter, cfg.FQDNTemplate, cfg.CombineFQDNAndAnnotation, cfg.IgnoreHostnameAnnotation)
	case "connector":
		return NewConnectorSource(cfg.ConnectorServer)
	case "crd":
		client, err := p.KubeClient()
		if err != nil {
			return nil, err
		}
		crdClient, scheme, err := NewCRDClientForAPIVersionKind(client, cfg.KubeConfig, cfg.APIServerURL, cfg.CRDSourceAPIVersion, cfg.CRDSourceKind)
		if err != nil {
			return nil, err
		}
		return NewCRDSource(crdClient, cfg.Namespace, cfg.CRDSourceKind, cfg.AnnotationFilter, cfg.LabelFilter, scheme, cfg.UpdateEvents)

	default:
		if fn, ok := SourceFn[source]; ok {
			return fn(ctx, source, p, srcCfg, config)
		}
	}

	return nil, ErrSourceNotFound
}

// If set, will instrument the K8S and other outgoing calls.
var InstrumentationWrapper  func(rt http.RoundTripper) http.RoundTripper

func instrumentedRESTConfig(kubeConfig, apiServerURL string, requestTimeout time.Duration) (*rest.Config, error) {
	config, err := GetRestConfig(kubeConfig, apiServerURL)
	if err != nil {
		return nil, err
	}
	config.WrapTransport = InstrumentationWrapper
	config.Timeout = requestTimeout
	return config, nil
}

// GetRestConfig returns the rest clients config to get automatically
// data if you run inside a cluster or by passing flags.
func GetRestConfig(kubeConfig, apiServerURL string) (*rest.Config, error) {
	if kubeConfig == "" {
		if _, err := os.Stat(clientcmd.RecommendedHomeFile); err == nil {
			kubeConfig = clientcmd.RecommendedHomeFile
		}
	}
	log.Debugf("apiServerURL: %s", apiServerURL)
	log.Debugf("kubeConfig: %s", kubeConfig)

	// evaluate whether to use kubeConfig-file or serviceaccount-token
	var (
		config *rest.Config
		err    error
	)
	if kubeConfig == "" {
		log.Infof("Using inCluster-config based on serviceaccount-token")
		config, err = rest.InClusterConfig()
	} else {
		log.Infof("Using kubeConfig")
		config, err = clientcmd.BuildConfigFromFlags(apiServerURL, kubeConfig)
	}
	if err != nil {
		return nil, err
	}

	return config, nil
}

// NewKubeClient returns a new Kubernetes client object. It takes a Config and
// uses APIServerURL and KubeConfig attributes to connect to the cluster. If
// KubeConfig isn't provided it defaults to using the recommended default.
func NewKubeClient(kubeConfig, apiServerURL string, requestTimeout time.Duration) (*kubernetes.Clientset, error) {
	config, err := instrumentedRESTConfig(kubeConfig, apiServerURL, requestTimeout)
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	log.Infof("Created Kubernetes client %s", config.Host)
	return client, nil
}

// NewIstioClient returns a new Istio client object. It uses the configured
// KubeConfig attribute to connect to the cluster. If KubeConfig isn't provided
// it defaults to using the recommended default.
// NB: Istio controls the creation of the underlying Kubernetes client, so we
// have no ability to tack on transport wrappers (e.g., MetricReader request
// wrappers) to the client's config at this level. Furthermore, the Istio client
// constructor does not expose the ability to override the Kubernetes API server endpoint,
// so the apiServerURL config attribute has no effect.
func NewIstioClient(kubeConfig string, apiServerURL string) (*istioclient.Clientset, error) {
	if kubeConfig == "" {
		if _, err := os.Stat(clientcmd.RecommendedHomeFile); err == nil {
			kubeConfig = clientcmd.RecommendedHomeFile
		}
	}

	restCfg, err := clientcmd.BuildConfigFromFlags(apiServerURL, kubeConfig)
	if err != nil {
		return nil, err
	}

	ic, err := istioclient.NewForConfig(restCfg)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to create istio client")
	}

	return ic, nil
}

// NewDynamicKubernetesClient returns a new Dynamic Kubernetes client object. It takes a Config and
// uses APIServerURL and KubeConfig attributes to connect to the cluster. If
// KubeConfig isn't provided it defaults to using the recommended default.
func NewDynamicKubernetesClient(kubeConfig, apiServerURL string, requestTimeout time.Duration) (dynamic.Interface, error) {
	config, err := instrumentedRESTConfig(kubeConfig, apiServerURL, requestTimeout)
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	log.Infof("Created Dynamic Kubernetes client %s", config.Host)
	return client, nil
}
