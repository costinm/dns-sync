package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"



// DNSRecord represents a CR that has host and IP information to program in DNS.
// It 'happens' to match Istio ServiceEntry - but it is intended as a pattern, more fields can be added to match other
// ways to represent the host and address info.
//
// Labels must be used to opt-in specific 'frontend' resources - it should never be expected that all ServiceEntries
// have frontend role by default.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +groupName=dns-resource
// +kubebuilder:resource:path=dns-resources
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +versionName=v1
type DNSRecord struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Records using standard syntax.
	RawRecords []string `json:"dns,omitempty"`

	Spec   ServiceEntrySpec   `json:"spec,omitempty"`
	Status ServiceEntryStatus `json:"status,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// DNSEndpointList is a list of DNSRecord objects
type DNSRecordList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DNSRecord `json:"items"`
}


// Must be named types.go (for consistency)

type Port struct {
	Name string `json:"name,omitempty"`
	Number int `json:"number,omitempty"`
}

type ServiceEntrySpec struct {
	// Hosts is the list of hostnames associated with the record.
	// Short names will be qualified with the namespace and mesh suffix or .svc.cluster.local
	// Only values that are allowed will be programmed into DNS.
	Hosts []string `json:"hosts"`

	// Addresses are the VIPs for the hosts.
	// If empty, this is a 'headless' record, using the endpoint addresses and 'original dst' for TCP or hostname for HTTP
	Addresses []string `json:"addresses"`

	// Ports can be used to generate SRV records.
	Ports []Port `json:"ports"`

	// SubjectAltNames provides info about the expected SANs - secure DNS (DNS-SEC, DOT, etc) is expected.
	SubjectAltNames []string `json:"subjectAltNames"`

}

type Address struct {
	Host string `json:"host,omitempty"`

	// Value is the address - IP for Istio.
	Value string `json:"value,omitempty"`
}

type ServiceEntryStatus struct {

	// Resource Generation to which the Reconciled Condition refers.
	// When this value is not equal to the object's metadata generation, reconciled condition  calculation for the current
	// generation is still in progress.  See https://istio.io/latest/docs/reference/config/config-status/ for more info.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	Addresses []Address `json:"addresses,omitempty"`
}


