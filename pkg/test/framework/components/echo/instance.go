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

package echo

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource"
)

// Instance is a component that provides access to a deployed echo service.
type Instance interface {
	Caller
	resource.Resource

	// Config returns the configuration of the Echo instance.
	Config() Config

	// Address of the service (e.g. Kubernetes cluster IP). May be "" if headless.
	Address() string

	// Workloads retrieves the list of all deployed workloads for this Echo service.
	// Guarantees at least one workload, if error == nil.
	Workloads() ([]Workload, error)
	WorkloadsOrFail(t test.Failer) []Workload

	// Restart restarts the workloads associated with this echo instance
	Restart() error
}
