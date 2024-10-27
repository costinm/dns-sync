package endpoint

import (
	"reflect"
	"sort"
)

type byNames ProviderSpecific

func (p byNames) Len() int           { return len(p) }
func (p byNames) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p byNames) Less(i, j int) bool { return p[i].Name < p[j].Name }

type byAllFields []*Endpoint

func (b byAllFields) Len() int      { return len(b) }
func (b byAllFields) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b byAllFields) Less(i, j int) bool {
	if b[i].DNSName < b[j].DNSName {
		return true
	}
	if b[i].DNSName == b[j].DNSName {
		// This rather bad, we need a more complex comparison for Targets, which considers all elements
		if b[i].Targets.Same(b[j].Targets) {
			if b[i].RecordType == (b[j].RecordType) {
				sa := b[i].ProviderSpecific
				sb := b[j].ProviderSpecific
				sort.Sort(byNames(sa))
				sort.Sort(byNames(sb))
				return reflect.DeepEqual(sa, sb)
			}
			return b[i].RecordType <= b[j].RecordType
		}
		return b[i].Targets.String() <= b[j].Targets.String()
	}
	return false
}

// SameEndpoint returns true if two endpoints are same
// considers example.org. and example.org DNSName/Target as different endpoints
func SameEndpoint(a, b *Endpoint) bool {
	return a.DNSName == b.DNSName && a.Targets.Same(b.Targets) && a.RecordType == b.RecordType && a.SetIdentifier == b.SetIdentifier &&
			a.Labels[OwnerLabelKey] == b.Labels[OwnerLabelKey] && a.RecordTTL == b.RecordTTL &&
			a.Labels[ResourceLabelKey] == b.Labels[ResourceLabelKey] &&
			a.Labels[OwnedRecordLabelKey] == b.Labels[OwnedRecordLabelKey] &&
			SameProviderSpecific(a.ProviderSpecific, b.ProviderSpecific)
}

// SameEndpoints compares two slices of endpoints regardless of order
// [x,y,z] == [z,x,y]
// [x,x,z] == [x,z,x]
// [x,y,y] != [x,x,y]
// [x,x,x] != [x,x,z]
func SameEndpoints(a, b []*Endpoint) bool {
	if len(a) != len(b) {
		return false
	}

	sa := a
	sb := b
	sort.Sort(byAllFields(sa))
	sort.Sort(byAllFields(sb))

	for i := range sa {
		if !SameEndpoint(sa[i], sb[i]) {
			return false
		}
	}
	return true
}

// SameEndpointLabels verifies that labels of the two slices of endpoints are the same
func SameEndpointLabels(a, b []*Endpoint) bool {
	if len(a) != len(b) {
		return false
	}

	sa := a
	sb := b
	sort.Sort(byAllFields(sa))
	sort.Sort(byAllFields(sb))

	for i := range sa {
		if !reflect.DeepEqual(sa[i].Labels, sb[i].Labels) {
			return false
		}
	}
	return true
}

// SamePlanChanges verifies that two set of changes are the same
func SamePlanChanges(a, b map[string][]*Endpoint) bool {
	return SameEndpoints(a["Create"], b["Create"]) && SameEndpoints(a["Delete"], b["Delete"]) &&
			SameEndpoints(a["UpdateOld"], b["UpdateOld"]) && SameEndpoints(a["UpdateNew"], b["UpdateNew"])
}

// SameProviderSpecific verifies that two maps contain the same string/string key/value pairs
func SameProviderSpecific(a, b ProviderSpecific) bool {
	sa := a
	sb := b
	sort.Sort(byNames(sa))
	sort.Sort(byNames(sb))
	return reflect.DeepEqual(sa, sb)
}
