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

package pilot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/controller/workloadentry"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/test/echo/check"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	echocommon "istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/echo/kube"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/pilot/common"
)

func GetAdditionVMImages() []string {
	out := []echo.VMDistro{}
	for distro, image := range kube.VMImages {
		if distro == echo.DefaultVMDistro {
			continue
		}
		out = append(out, image)
	}
	return out
}

func TestVmOSPost(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.reachability").
		Label(label.Postsubmit).
		Run(func(t framework.TestContext) {
			if t.Settings().Skip(echo.VM) {
				t.Skip("VM tests are disabled")
			}
			b := echoboot.NewBuilder(t, t.Clusters().Primaries().Default())
			images := GetAdditionVMImages()
			for _, image := range images {
				b = b.WithConfig(echo.Config{
					Service:    "vm-" + strings.ReplaceAll(image, "_", "-"),
					Namespace:  apps.Namespace,
					Ports:      echocommon.EchoPorts,
					DeployAsVM: true,
					VMDistro:   image,
					Subsets:    []echo.SubsetConfig{{}},
				})
			}
			instances := b.BuildOrFail(t)

			for i, image := range images {
				i, image := i, image
				t.NewSubTest(image).RunParallel(func(t framework.TestContext) {
					for _, tt := range common.VMTestCases(echo.Instances{instances[i]}, apps) {
						tt.Run(t, apps.Namespace.Name())
					}
				})
			}
		})
}

func TestVMRegistrationLifecycle(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/33154")
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Features("vm.autoregistration").
		Run(func(t framework.TestContext) {
			if t.Settings().Skip(echo.VM) {
				t.Skip()
			}
			scaleDeploymentOrFail(t, "istiod", i.Settings().SystemNamespace, 2)
			client := apps.PodA.GetOrFail(t, echo.InCluster(t.Clusters().Default()))
			// TODO test multi-network (must be shared control plane but on different networks)
			var autoVM echo.Instance
			_ = echoboot.NewBuilder(t).
				With(&autoVM, echo.Config{
					Namespace:      apps.Namespace,
					Service:        "auto-vm",
					Ports:          echocommon.EchoPorts,
					DeployAsVM:     true,
					AutoRegisterVM: true,
				}).BuildOrFail(t)
			t.NewSubTest("initial registration").Run(func(t framework.TestContext) {
				retry.UntilSuccessOrFail(t, func() error {
					res, err := client.Call(echo.CallOptions{
						Target: autoVM,
						Port:   &autoVM.Config().Ports[0],
						Retry: echo.Retry{
							NoRetry: true,
						},
					})
					return check.And(
						check.NoError(),
						check.OK()).Check(res, err)
				}, retry.Timeout(15*time.Second))
			})
			t.NewSubTest("reconnect reuses WorkloadEntry").Run(func(t framework.TestContext) {
				// ensure we have two pilot instances, other tests can pass before the second one comes up
				retry.UntilSuccessOrFail(t, func() error {
					pilotRes, err := t.Clusters().Default().CoreV1().Pods(i.Settings().SystemNamespace).
						List(context.TODO(), metav1.ListOptions{LabelSelector: "istio=pilot"})
					if err != nil {
						return err
					}
					if len(pilotRes.Items) != 2 {
						return errors.New("expected 2 pilots")
					}
					return nil
				}, retry.Timeout(10*time.Second))

				// get the initial workload entry state
				entries := getWorkloadEntriesOrFail(t, autoVM)
				if len(entries) != 1 {
					t.Fatalf("expected exactly 1 WorkloadEntry but got %d", len(entries))
				}
				initialWLE := entries[0]

				// keep force-disconnecting until we observe a reconnect to a different istiod instance
				initialPilot := initialWLE.Annotations[workloadentry.WorkloadControllerAnnotation]
				disconnectProxy(t, initialPilot, autoVM)
				retry.UntilSuccessOrFail(t, func() error {
					entries := getWorkloadEntriesOrFail(t, autoVM)
					if len(entries) != 1 || entries[0].UID != initialWLE.UID {
						t.Fatalf("WorkloadEntry was cleaned up unexpectedly")
					}

					currentPilot := entries[0].Annotations[workloadentry.WorkloadControllerAnnotation]
					if currentPilot == initialPilot || !strings.HasPrefix(currentPilot, "istiod-") {
						disconnectProxy(t, currentPilot, autoVM)
						return errors.New("expected WorkloadEntry to be updated by other pilot")
					}
					return nil
				}, retry.Delay(5*time.Second))
			})
			t.NewSubTest("disconnect deletes WorkloadEntry").Run(func(t framework.TestContext) {
				deployment := fmt.Sprintf("%s-%s", autoVM.Config().Service, "v1")
				scaleDeploymentOrFail(t, deployment, autoVM.Config().Namespace.Name(), 0)
				// it should take at most just over GracePeriod to cleanup if all pilots are healthy
				retry.UntilSuccessOrFail(t, func() error {
					if len(getWorkloadEntriesOrFail(t, autoVM)) > 0 {
						return errors.New("expected 0 WorkloadEntries")
					}
					return nil
				}, retry.Timeout(2*features.WorkloadEntryCleanupGracePeriod+(2*time.Second)))
			})
		})
}

func disconnectProxy(t framework.TestContext, pilot string, instance echo.Instance) {
	proxyID := strings.Join([]string{instance.WorkloadsOrFail(t)[0].PodName(), instance.Config().Namespace.Name()}, ".")
	cmd := "pilot-discovery request GET /debug/force_disconnect?proxyID=" + proxyID
	stdOut, _, err := t.Clusters().Default().
		PodExec(pilot, i.Settings().SystemNamespace, "discovery", cmd)
	if err != nil {
		scopes.Framework.Warnf("failed to force disconnect %s: %v: %v", proxyID, stdOut, err)
	}
}

func scaleDeploymentOrFail(t framework.TestContext, name, namespace string, scale int32) {
	s, err := t.Clusters().Default().AppsV1().Deployments(namespace).
		GetScale(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s.Spec.Replicas = scale
	_, err = t.Clusters().Default().AppsV1().Deployments(namespace).
		UpdateScale(context.TODO(), name, s, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
}

func getWorkloadEntriesOrFail(t framework.TestContext, vm echo.Instance) []v1alpha3.WorkloadEntry {
	res, err := t.Clusters().Default().Istio().NetworkingV1alpha3().
		WorkloadEntries(vm.Config().Namespace.Name()).
		List(context.TODO(), metav1.ListOptions{LabelSelector: "app=" + vm.Config().Service})
	if err != nil {
		t.Fatal(err)
	}
	return res.Items
}
