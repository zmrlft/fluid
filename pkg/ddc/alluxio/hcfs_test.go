/*
Copyright 2021 The Fluid Authors.

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

package alluxio

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/brahma-adshonor/gohook"
	v1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// newAlluxioEngineHCFS creates a new instance of AlluxioEngine with the provided client,
// name, and namespace. It initializes the runtime, runtime information, and logger
// for the engine, and returns the initialized AlluxioEngine instance.
//
// Parameters:
//   - client: The Kubernetes client used to interact with the cluster.
//   - name: The name of the Alluxio engine.
//   - namespace: The namespace where the Alluxio engine will be deployed.
//
// Returns:
//   - *AlluxioEngine: A pointer to the newly created AlluxioEngine instance.
func newAlluxioEngineHCFS(client client.Client, name string, namespace string) *AlluxioEngine {
	runTime := &v1alpha1.AlluxioRuntime{}
	runTimeInfo, _ := base.BuildRuntimeInfo(name, namespace, "alluxio")
	engine := &AlluxioEngine{
		runtime:     runTime,
		name:        name,
		namespace:   namespace,
		Client:      client,
		runtimeInfo: runTimeInfo,
		Log:         fake.NullLogger(),
	}
	return engine
}

// TestGetHCFSStatus tests various scenarios of the GetHCFSStatus method.
// This test verifies the logic of retrieving HCFS status, covering the following cases:
// 1. In the normal case, it should correctly return the expected endpoint and filesystem version.
// 2. When the service is not registered, GetHCFSStatus should return an appropriate error.
// 3. When an error occurs during configuration retrieval, GetHCFSStatus should return the error.
// The test uses mocks of kubeclient.ExecCommandInContainerWithFullOutput to simulate different outputs.
func TestGetHCFSStatus(t *testing.T) {
	mockExecCommon := func(ctx context.Context, podName string, containerName string, namespace string, cmd []string) (stdout string, stderr string, e error) {
		return "conf", "", nil
	}
	mockExecErr := func(ctx context.Context, podName string, containerName string, namespace string, cmd []string) (stdout string, stderr string, e error) {
		return "err", "", errors.New("other error")
	}
	wrappedUnhook := func() {
		err := gohook.UnHook(kubeclient.ExecCommandInContainerWithFullOutput)
		if err != nil {
			t.Fatal(err.Error())
		}
	}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "hbase-master-0",
			Namespace:   "fluid",
			Annotations: common.GetExpectedFluidAnnotations(),
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "rpc",
					Port: 2333,
				},
			},
		},
	}
	serviceWithErr := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "not-register-master-0",
			Namespace:   "fluid",
			Annotations: common.GetExpectedFluidAnnotations(),
		},
	}
	runtimeObjs := []runtime.Object{}
	runtimeObjs = append(runtimeObjs, service.DeepCopy())
	runtimeObjs = append(runtimeObjs, serviceWithErr.DeepCopy())
	fakeClient := fake.NewFakeClientWithScheme(testScheme, runtimeObjs...)
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(v1.SchemeGroupVersion, service)
	fakeClientWithErr := fake.NewFakeClientWithScheme(scheme, runtimeObjs...)

	// test common case
	err := gohook.Hook(kubeclient.ExecCommandInContainerWithFullOutput, mockExecCommon, nil)
	if err != nil {
		t.Fatal(err.Error())
	}
	engine := newAlluxioEngineHCFS(fakeClient, "hbase", "fluid")
	out, _ := engine.GetHCFSStatus()
	wrappedUnhook()
	status := &v1alpha1.HCFSStatus{
		Endpoint:                    "alluxio://hbase-master-0.fluid:2333",
		UnderlayerFileSystemVersion: "conf",
	}
	if !reflect.DeepEqual(*out, *status) {
		t.Errorf("status message wrong!")
	}

	// test when not register case
	engine = newAlluxioEngineHCFS(fakeClientWithErr, "hbase", "fluid")
	_, err = engine.GetHCFSStatus()
	if err == nil {
		t.Errorf("expect No Register Err, but not got.")
	}

	// test when getConf with err
	err = gohook.Hook(kubeclient.ExecCommandInContainerWithFullOutput, mockExecErr, nil)
	if err != nil {
		t.Fatal(err.Error())
	}
	engine = newAlluxioEngineHCFS(fakeClient, "hbase", "fluid")
	_, err = engine.GetHCFSStatus()
	wrappedUnhook()
	if err == nil {
		t.Errorf("expect get Conf Err, but not got.")
	}

}

// TestQueryHCFSEndpoint verifies the behavior of AlluxioEngine's HCFS endpoint query functionality.
// This test validates three main scenarios:
// 1. Service Not Found: When the specified Service resource doesn't exist in the cluster
// 2. Unregistered Service: When the Service exists but lacks proper registration (invalid scheme configuration)
// 3. Normal Case: When a properly configured Service exists with expected annotations and port configuration
// Setup:
// - Creates mock Service resources with different configurations:
// * Valid service "hbase-master-0" with port 2333 and fluid annotations
// * Invalid service "not-register-master-0" without proper registration
// - Uses two fake Kubernetes clients:
// * Normal client with complete scheme configuration
// * Error-injected client with incomplete scheme to simulate registration issues
func TestQueryHCFSEndpoint(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "hbase-master-0",
			Namespace:   "fluid",
			Annotations: common.GetExpectedFluidAnnotations(),
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "rpc",
					Port: 2333,
				},
			},
		},
	}
	serviceWithErr := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "not-register-master-0",
			Namespace:   "fluid",
			Annotations: common.GetExpectedFluidAnnotations(),
		},
	}
	runtimeObjs := []runtime.Object{}
	runtimeObjs = append(runtimeObjs, service.DeepCopy())
	runtimeObjs = append(runtimeObjs, serviceWithErr.DeepCopy())
	fakeClient := fake.NewFakeClientWithScheme(testScheme, runtimeObjs...)
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(v1.SchemeGroupVersion, service)
	fakeClientWithErr := fake.NewFakeClientWithScheme(scheme, runtimeObjs...)
	testCases := []struct {
		name      string
		namespace string
		out       string
		isErr     bool
	}{
		{
			name:      "not-found",
			namespace: "fluid",
			out:       "",
			isErr:     false,
		},
		{
			name:      "not-register",
			namespace: "fluid",
			out:       "",
			isErr:     false,
		},
		{
			name:      "hbase",
			namespace: "fluid",
			out:       "alluxio://hbase-master-0.fluid:2333",
			isErr:     false,
		},
	}
	for _, testCase := range testCases {
		engine := newAlluxioEngineHCFS(fakeClient, testCase.name, testCase.namespace)
		if testCase.name == "not-register" {
			engine = newAlluxioEngineHCFS(fakeClientWithErr, testCase.name, testCase.namespace)
		}
		out, err := engine.queryHCFSEndpoint()
		if out != testCase.out {
			t.Errorf("input parameter is %s,expected %s, got %s", testCase.name, testCase.out, out)
		}
		isErr := err != nil
		if isErr != testCase.isErr {
			t.Errorf("input parameter is %s,expected %t, got %t", testCase.name, testCase.isErr, isErr)
		}
	}
}

// TestCompatibleUFSVersion tests the compatibility of the UFS (Under File System) version
// by mocking the execution of commands in a container. It verifies that the function
// queryCompatibleUFSVersion returns the expected output based on the mocked command execution results.
func TestCompatibleUFSVersion(t *testing.T) {
	mockExecCommon := func(ctx context.Context, podName string, containerName string, namespace string, cmd []string) (stdout string, stderr string, e error) {
		return "conf", "", nil
	}
	mockExecErr := func(ctx context.Context, podName string, containerName string, namespace string, cmd []string) (stdout string, stderr string, e error) {
		return "err", "", errors.New("other error")
	}
	wrappedUnhook := func() {
		err := gohook.UnHook(kubeclient.ExecCommandInContainerWithFullOutput)
		if err != nil {
			t.Fatal(err.Error())
		}
	}
	err := gohook.Hook(kubeclient.ExecCommandInContainerWithFullOutput, mockExecCommon, nil)
	if err != nil {
		t.Fatal(err.Error())
	}
	engine := newAlluxioEngineHCFS(nil, "hbase", "fluid")
	out, _ := engine.queryCompatibleUFSVersion()
	if out != "conf" {
		t.Errorf("expected %s, got %s", "conf", out)
	}
	wrappedUnhook()
	err = gohook.Hook(kubeclient.ExecCommandInContainerWithFullOutput, mockExecErr, nil)
	if err != nil {
		t.Fatal(err.Error())
	}
	engine = newAlluxioEngineHCFS(nil, "hbase", "fluid")
	out, _ = engine.queryCompatibleUFSVersion()
	if out != "err" {
		t.Errorf("expected %s, got %s", "err", out)
	}
	wrappedUnhook()
}
