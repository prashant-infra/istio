//go:build integ
// +build integ

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

package common

import (
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
)

type EchoDeployments struct {
	// Namespace echo apps will be deployed
	Namespace namespace.Instance
	// Namespace where external echo app will be deployed
	ExternalNamespace namespace.Instance

	// Ingressgateway instance
	Ingress   ingress.Instance
	Ingresses ingress.Instances

	// Standard echo app to be used by tests
	PodA echo.Instances
	// Standard echo app to be used by tests
	PodB echo.Instances
	// Standard echo app to be used by tests
	PodC echo.Instances
	// Standard echo app with TPROXY interception mode to be used by tests
	PodTproxy echo.Instances
	// Headless echo app to be used by tests
	Headless echo.Instances
	// StatefulSet echo app to be used by tests
	StatefulSet echo.Instances
	// ProxylessGRPC echo app to be used by tests
	ProxylessGRPC echo.Instances
	// Echo app to be used by tests, with no sidecar injected
	Naked echo.Instances
	// A virtual machine echo app (only deployed to one cluster)
	VM echo.Instances
	// DeltaXDS echo app uses the delta XDS protocol. This should be functionally equivalent to PodA.
	DeltaXDS echo.Instances

	// Echo app to be used by tests, with no sidecar injected
	External echo.Instances

	All echo.Instances
}

const (
	PodASvc          = "a"
	PodBSvc          = "b"
	PodCSvc          = "c"
	PodTproxySvc     = "tproxy"
	VMSvc            = "vm"
	HeadlessSvc      = "headless"
	StatefulSetSvc   = "statefulset"
	ProxylessGRPCSvc = "proxyless-grpc"
	NakedSvc         = "naked"
	ExternalSvc      = "external"
	DeltaSvc         = "delta"

	externalHostname = "fake.external.com"
)

func FindPortByName(name string) echo.Port {
	for _, p := range common.EchoPorts {
		if p.Name == name {
			return p
		}
	}
	return echo.Port{}
}

func serviceEntryPorts() []echo.Port {
	res := []echo.Port{}
	for _, p := range common.EchoPorts {
		if strings.HasPrefix(p.Name, "auto") {
			// The protocol needs to be set in common.EchoPorts to configure the echo deployment
			// But for service entry, we want to ensure we set it to "" which will use sniffing
			p.Protocol = ""
		}
		res = append(res, p)
	}
	return res
}

func SetupApps(t resource.Context, i istio.Instance, apps *EchoDeployments) error {
	var err error
	apps.Namespace, err = namespace.New(t, namespace.Config{
		Prefix: "echo",
		Inject: true,
	})
	if err != nil {
		return err
	}
	apps.ExternalNamespace, err = namespace.New(t, namespace.Config{
		Prefix: "external",
		Inject: false,
	})
	if err != nil {
		return err
	}

	apps.Ingress = i.IngressFor(t.Clusters().Default())
	apps.Ingresses = i.Ingresses()

	// Headless services don't work with targetPort, set to same port
	headlessPorts := make([]echo.Port, len(common.EchoPorts))
	for i, p := range common.EchoPorts {
		p.ServicePort = p.InstancePort
		headlessPorts[i] = p
	}
	builder := echoboot.NewBuilder(t).
		WithClusters(t.Clusters()...).
		WithConfig(echo.Config{
			Service:           PodASvc,
			Namespace:         apps.Namespace,
			Ports:             common.EchoPorts,
			Subsets:           []echo.SubsetConfig{{}},
			Locality:          "region.zone.subzone",
			WorkloadOnlyPorts: common.WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:           PodBSvc,
			Namespace:         apps.Namespace,
			Ports:             common.EchoPorts,
			Subsets:           []echo.SubsetConfig{{}},
			WorkloadOnlyPorts: common.WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:           PodCSvc,
			Namespace:         apps.Namespace,
			Ports:             common.EchoPorts,
			Subsets:           []echo.SubsetConfig{{}},
			WorkloadOnlyPorts: common.WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:           HeadlessSvc,
			Headless:          true,
			Namespace:         apps.Namespace,
			Ports:             headlessPorts,
			Subsets:           []echo.SubsetConfig{{}},
			WorkloadOnlyPorts: common.WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:           StatefulSetSvc,
			Headless:          true,
			StatefulSet:       true,
			Namespace:         apps.Namespace,
			Ports:             headlessPorts,
			Subsets:           []echo.SubsetConfig{{}},
			WorkloadOnlyPorts: common.WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:   NakedSvc,
			Namespace: apps.Namespace,
			Ports:     common.EchoPorts,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarInject: {
							Value: strconv.FormatBool(false),
						},
					},
				},
			},
			WorkloadOnlyPorts: common.WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:           ExternalSvc,
			Namespace:         apps.ExternalNamespace,
			DefaultHostHeader: externalHostname,
			Ports:             common.EchoPorts,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarInject: {
							Value: strconv.FormatBool(false),
						},
					},
				},
			},
			WorkloadOnlyPorts: common.WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:   PodTproxySvc,
			Namespace: apps.Namespace,
			Ports:     common.EchoPorts,
			Subsets: []echo.SubsetConfig{{
				Annotations: echo.NewAnnotations().Set(echo.SidecarInterceptionMode, "TPROXY"),
			}},
			WorkloadOnlyPorts: common.WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:           VMSvc,
			Namespace:         apps.Namespace,
			Ports:             common.EchoPorts,
			DeployAsVM:        true,
			AutoRegisterVM:    true,
			Subsets:           []echo.SubsetConfig{{}},
			WorkloadOnlyPorts: common.WorkloadPorts,
		})

	skipDelta := t.Settings().Skip(echo.Delta) || !t.Settings().Revisions.AtLeast("1.11")
	if !skipDelta {
		builder = builder.
			WithConfig(echo.Config{
				Service:   DeltaSvc,
				Namespace: apps.Namespace,
				Ports:     common.EchoPorts,
				Subsets: []echo.SubsetConfig{{
					Annotations: echo.NewAnnotations().Set(echo.SidecarProxyConfig, `proxyMetadata:
  ISTIO_DELTA_XDS: "true"`),
				}},
				WorkloadOnlyPorts: common.WorkloadPorts,
			})
	}

	if !t.Clusters().IsMulticluster() {
		builder = builder.
			// TODO when agent handles secure control-plane connection for grpc-less, deploy to "remote" clusters
			WithConfig(echo.Config{
				Service:   ProxylessGRPCSvc,
				Namespace: apps.Namespace,
				Ports:     common.EchoPorts,
				Subsets: []echo.SubsetConfig{
					{
						Annotations: map[echo.Annotation]*echo.AnnotationValue{
							echo.SidecarInjectTemplates: {
								Value: "grpc-agent",
							},
						},
					},
				},
				WorkloadOnlyPorts: common.WorkloadPorts,
			})
	}

	echos, err := builder.Build()
	if err != nil {
		return err
	}
	apps.All = echos
	apps.PodA = echos.Match(echo.Service(PodASvc))
	apps.PodB = echos.Match(echo.Service(PodBSvc))
	apps.PodC = echos.Match(echo.Service(PodCSvc))
	apps.PodTproxy = echos.Match(echo.Service(PodTproxySvc))
	apps.Headless = echos.Match(echo.Service(HeadlessSvc))
	apps.StatefulSet = echos.Match(echo.Service(StatefulSetSvc))
	apps.Naked = echos.Match(echo.Service(NakedSvc))
	apps.External = echos.Match(echo.Service(ExternalSvc))
	apps.ProxylessGRPC = echos.Match(echo.Service(ProxylessGRPCSvc))
	if !t.Settings().Skip(echo.VM) {
		apps.VM = echos.Match(echo.Service(VMSvc))
	}
	if !skipDelta {
		apps.DeltaXDS = echos.Match(echo.Service(DeltaSvc))
	}

	if err := t.ConfigIstio().YAML(`
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: restrict-to-namespace
spec:
  egress:
  - hosts:
    - "./*"
    - "istio-system/*"
`).Apply(apps.Namespace.Name(), resource.NoCleanup); err != nil {
		return err
	}

	se, err := tmpl.Evaluate(`apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: external-service
spec:
  hosts:
  - {{.Hostname}}
  location: MESH_EXTERNAL
  resolution: DNS
  endpoints:
  - address: external.{{.Namespace}}.svc.cluster.local
  ports:
  - name: http-tls-origination
    number: 8888
    protocol: http
    targetPort: 443
  - name: http2-tls-origination
    number: 8882
    protocol: http2
    targetPort: 443
{{- range $i, $p := .Ports }}
  - name: {{$p.Name}}
    number: {{$p.ServicePort}}
    protocol: "{{$p.Protocol}}"
{{- end }}
`, map[string]interface{}{"Namespace": apps.ExternalNamespace.Name(), "Hostname": externalHostname, "Ports": serviceEntryPorts()})
	if err != nil {
		return err
	}
	if err := t.ConfigIstio().YAML(se).Apply(apps.Namespace.Name(), resource.NoCleanup); err != nil {
		return err
	}
	return nil
}

func (d EchoDeployments) IsMulticluster() bool {
	return d.All.Clusters().IsMulticluster()
}

// Restart restarts all echo deployments.
func (d EchoDeployments) Restart() error {
	wg := sync.WaitGroup{}
	aggregateErrMux := &sync.Mutex{}
	var aggregateErr error
	for _, app := range d.All {
		app := app
		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := app.Restart(); err != nil {
				aggregateErrMux.Lock()
				aggregateErr = multierror.Append(aggregateErr, err)
				aggregateErrMux.Unlock()
			}
		}()
	}
	wg.Wait()
	if aggregateErr != nil {
		return aggregateErr
	}
	return nil
}
