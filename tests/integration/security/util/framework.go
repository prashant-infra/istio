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

package util

import (
	"fmt"
	"os"
	"path"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	ASvc             = "a"
	BSvc             = "b"
	CSvc             = "c"
	DSvc             = "d"
	ESvc             = "e"
	MultiversionSvc  = "multiversion"
	VMSvc            = "vm"
	HeadlessSvc      = "headless"
	NakedSvc         = "naked"
	HeadlessNakedSvc = "headless-naked"
	ExternalSvc      = "external"

	// CallsPerCluster is used to ensure cross-cluster load balancing has a chance to work
	CallsPerCluster = 5
)

type EchoDeployments struct {
	// TODO: Consolidate the echo config and reduce/reuse echo instances (https://github.com/istio/istio/issues/28599)
	// Namespace1 is used as the default namespace for reachability tests and other tests which can reuse the same config for echo instances
	Namespace1 namespace.Instance
	// Namespace2 is used by most authorization test cases within authorization_test.go
	Namespace2 namespace.Instance
	// Namespace3 is used by TestAuthorization_Conditions and there is one C echo instance deployed
	Namespace3    namespace.Instance
	A, B, C, D, E echo.Instances
	Multiversion  echo.Instances
	Headless      echo.Instances
	Naked         echo.Instances
	VM            echo.Instances
	HeadlessNaked echo.Instances
	All           echo.Instances
	External      echo.Instances
}

func EchoConfig(name string, ns namespace.Instance, headless bool, annos echo.Annotations) echo.Config {
	out := echo.Config{
		Service:        name,
		Namespace:      ns,
		ServiceAccount: true,
		Headless:       headless,
		Subsets: []echo.SubsetConfig{
			{
				Version:     "v1",
				Annotations: annos,
			},
		},
		Ports: []echo.Port{
			{
				Name:     "http",
				Protocol: protocol.HTTP,
				// We use a port > 1024 to not require root
				InstancePort: 8090,
				ServicePort:  8095,
			},
			{
				Name:     "tcp",
				Protocol: protocol.TCP,
			},
			{
				Name:     "grpc",
				Protocol: protocol.GRPC,
			},
			{
				Name:         "https",
				Protocol:     protocol.HTTPS,
				ServicePort:  443,
				InstancePort: 8443,
				TLS:          true,
			},
			{
				Name:         "http-8091",
				Protocol:     protocol.HTTP,
				InstancePort: 8091,
			},
			{
				Name:         "http-8092",
				Protocol:     protocol.HTTP,
				InstancePort: 8092,
			},
			{
				Name:         "tcp-8093",
				Protocol:     protocol.TCP,
				InstancePort: 8093,
			},
			{
				Name:         "tcp-8094",
				Protocol:     protocol.TCP,
				InstancePort: 8094,
			},
		},
		// Workload Ports needed by TestPassThroughFilterChain
		// The port 8084-8089 will be defined only in the workload and not in the k8s service.
		WorkloadOnlyPorts: []echo.WorkloadPort{
			{
				Port:     8085,
				Protocol: protocol.HTTP,
			},
			{
				Port:     8086,
				Protocol: protocol.HTTP,
			},
			{
				Port:     8087,
				Protocol: protocol.TCP,
			},
			{
				Port:     8088,
				Protocol: protocol.TCP,
			},
			{
				Port:     8089,
				Protocol: protocol.HTTPS,
				TLS:      true,
			},
			{
				Port:     8084,
				Protocol: protocol.HTTPS,
				TLS:      true,
			},
		},
	}

	// for headless service with selector, the port and target port must be equal
	// Ref: https://kubernetes.io/docs/concepts/services-networking/service/#headless-services
	if headless {
		for i := range out.Ports {
			out.Ports[i].ServicePort = out.Ports[i].InstancePort
		}
	}
	return out
}

func MustReadCert(f string) string {
	b, err := os.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs/dns", f))
	if err != nil {
		panic(fmt.Sprintf("failed to read %v: %v", f, err))
	}
	return string(b)
}

func SetupApps(ctx resource.Context, i istio.Instance, apps *EchoDeployments, buildVM bool) error {
	if ctx.Settings().Skip(echo.VM) {
		buildVM = false
	}
	var err error
	apps.Namespace1, err = namespace.New(ctx, namespace.Config{
		Prefix: "test-ns1",
		Inject: true,
	})
	if err != nil {
		return err
	}
	apps.Namespace2, err = namespace.New(ctx, namespace.Config{
		Prefix: "test-ns2",
		Inject: true,
	})
	if err != nil {
		return err
	}
	apps.Namespace3, err = namespace.New(ctx, namespace.Config{
		Prefix: "test-ns3",
		Inject: true,
	})
	if err != nil {
		return err
	}

	builder := echoboot.NewBuilder(ctx).
		WithClusters(ctx.Clusters()...).
		WithConfig(EchoConfig(ASvc, apps.Namespace1, false, nil)).
		WithConfig(EchoConfig(BSvc, apps.Namespace1, false, nil)).
		WithConfig(EchoConfig(CSvc, apps.Namespace1, false, nil)).
		WithConfig(EchoConfig(DSvc, apps.Namespace1, false, nil)).
		WithConfig(EchoConfig(ESvc, apps.Namespace1, false, nil)).
		WithConfig(func() echo.Config {
			// Multi-version specific setup
			multiVersionCfg := EchoConfig(MultiversionSvc, apps.Namespace1, false, nil)
			multiVersionCfg.Subsets = []echo.SubsetConfig{
				// Istio deployment, with sidecar.
				{
					Version: "vistio",
				},
				// Legacy deployment subset, does not have sidecar injected.
				{
					Version:     "vlegacy",
					Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
				},
			}
			return multiVersionCfg
		}()).
		WithConfig(EchoConfig(NakedSvc, apps.Namespace1, false, echo.NewAnnotations().
			SetBool(echo.SidecarInject, false))).
		WithConfig(EchoConfig(BSvc, apps.Namespace2, false, nil)).
		WithConfig(EchoConfig(CSvc, apps.Namespace2, false, nil)).
		WithConfig(EchoConfig(ESvc, apps.Namespace2, false, nil)).
		WithConfig(EchoConfig(CSvc, apps.Namespace3, false, nil)).
		WithConfig(func() echo.Config {
			// VM specific setup
			vmCfg := EchoConfig(VMSvc, apps.Namespace1, false, nil)
			// for test cases that have `buildVM` off, vm will function like a regular pod
			vmCfg.DeployAsVM = buildVM
			return vmCfg
		}()).
		WithConfig(echo.Config{
			Service:   ExternalSvc,
			Namespace: apps.Namespace1,
			Ports: []echo.Port{
				{
					// Plain HTTP port only used to route request to egress gateway
					Name:         "http",
					Protocol:     protocol.HTTP,
					ServicePort:  80,
					InstancePort: 8080,
				},
				{
					// HTTPS port
					Name:         "https",
					Protocol:     protocol.HTTPS,
					ServicePort:  443,
					InstancePort: 8443,
					TLS:          true,
				},
			},
			// Set up TLS certs on the server. This will make the server listen with these credentials.
			TLSSettings: &common.TLSSettings{
				// Echo has these test certs baked into the docker image
				RootCert:   MustReadCert("root-cert.pem"),
				ClientCert: MustReadCert("cert-chain.pem"),
				Key:        MustReadCert("key.pem"),
				// Override hostname to match the SAN in the cert we are using
				Hostname: "server.default.svc",
			},
			Subsets: []echo.SubsetConfig{{
				Version:     "v1",
				Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
			}},
		}).
		WithConfig(EchoConfig(HeadlessSvc, apps.Namespace1, true, nil)).
		WithConfig(EchoConfig(HeadlessNakedSvc, apps.Namespace1, true, echo.NewAnnotations().
			SetBool(echo.SidecarInject, false)))

	echos, err := builder.Build()
	if err != nil {
		return err
	}
	apps.All = echos
	apps.A = echos.Match(echo.Service(ASvc))
	apps.B = echos.Match(echo.Service(BSvc))
	apps.C = echos.Match(echo.Service(CSvc))
	apps.D = echos.Match(echo.Service(DSvc))
	apps.E = echos.Match(echo.Service(ESvc))

	apps.Multiversion = echos.Match(echo.Service(MultiversionSvc))
	apps.Headless = echos.Match(echo.Service(HeadlessSvc))
	apps.Naked = echos.Match(echo.Service(NakedSvc))
	apps.VM = echos.Match(echo.Service(VMSvc))
	apps.HeadlessNaked = echos.Match(echo.Service(HeadlessNakedSvc))

	return nil
}

func (apps *EchoDeployments) IsNaked(i echo.Instance) bool {
	return apps.HeadlessNaked.Contains(i) || apps.Naked.Contains(i)
}

func (apps *EchoDeployments) IsHeadless(i echo.Instance) bool {
	return apps.HeadlessNaked.Contains(i) || apps.Headless.Contains(i)
}

func (apps *EchoDeployments) IsVM(i echo.Instance) bool {
	return apps.VM.Contains(i)
}

// IsMultiversion matches instances that have Multi-version specific setup.
func IsMultiversion() echo.Matcher {
	return func(i echo.Instance) bool {
		if len(i.Config().Subsets) != 2 {
			return false
		}
		var matchIstio, matchLegacy bool
		for _, s := range i.Config().Subsets {
			if s.Version == "vistio" {
				matchIstio = true
			} else if s.Version == "vlegacy" && !s.Annotations.GetBool(echo.SidecarInject) {
				matchLegacy = true
			}
		}
		return matchIstio && matchLegacy
	}
}

// SourceFilter returns workload pod A with sidecar injected and VM
func SourceFilter(t framework.TestContext, apps *EchoDeployments, ns string, skipVM bool) []echotest.Filter {
	rt := []echotest.Filter{func(instances echo.Instances) echo.Instances {
		inst := apps.A.Match(echo.Namespace(ns))
		if !skipVM {
			inst = append(inst, apps.VM.Match(echo.Namespace(ns))...)
		}
		return inst
	}}
	return rt
}

// DestFilter returns workload pod B with sidecar injected and VM
func DestFilter(t framework.TestContext, apps *EchoDeployments, ns string, skipVM bool) []echotest.Filter {
	rt := []echotest.Filter{func(instances echo.Instances) echo.Instances {
		inst := apps.B.Match(echo.Namespace(ns))
		if !skipVM {
			inst = append(inst, apps.VM.Match(echo.Namespace(ns))...)
		}
		return inst
	}}
	return rt
}
