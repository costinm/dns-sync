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

package mem

import (
	"context"

	"github.com/costinm/dns-sync/endpoint"
)

// MemSource is a source backed by files or static data, mainly for testing.
type MemSource struct {
	Records []*endpoint.Endpoint

	Error error
	Handler func()
}

// Endpoints returns the desired mock endpoints.
func (m *MemSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	return m.Records, m.Error
}

// AddEventHandler adds an event handler that should be triggered if something in source changes
func (m *MemSource) AddEventHandler(ctx context.Context, handler func()) {
	m.Handler = handler
}
