package endpoint

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// DNSServiceSepc represents an provider using the external-dns webhook API.
//
type DNSServiceSpec struct {
	// Protocol used to communicate with the provider - one of the build
	// in implementations "aws", "azure", "gcp", "rfc2136", "route53",
	// "alidns", "cloudflare", "dnsimple", "dnsmadeeasy", "infoblox",
	// "linode", "namedotcom", "ovh", "rfc2136", "ultradns"...
	Protocol string `json:"protocol"`

	// URL to the provider's API endpoint, if not hardcoded by the protocol.
	// This will be the Webhook address for out-of-tree providers.
	Address string `json:"address"`

	Zones map[string]string `json:"zones"`
}

type DNSZone struct {
	Name string
	Domain string

}

type DNSSource struct {
	Name string
	Domain string

}

type DNSServiceStatus struct {
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DNSSync represents one sync between a set of sources and a single provider.
//
// The user-specified CRD should also have the status sub-resource.
// +k8s:openapi-gen=true
// +groupName=dns-sync
// +kubebuilder:resource:path=dnssyncs
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +versionName=v1

type DNSServiceProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DNSServiceSpec   `json:"spec,omitempty"`
	Status DNSServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// DNSEndpointList is a list of DNSEndpoint objects
type DNSServiceProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DNSServiceProvider `json:"items"`
}
