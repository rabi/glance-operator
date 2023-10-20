/*
Copyright 2023.

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

package functional

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openstack-k8s-operators/lib-common/modules/common/condition"
	. "github.com/openstack-k8s-operators/lib-common/modules/common/test/helpers"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Glanceapi controller", func() {
	When("GlanceAPI CR is created", func() {
		BeforeEach(func() {
			DeferCleanup(th.DeleteInstance, CreateGlanceAPI(glanceTest.GlanceSingle, GetDefaultGlanceAPISpec(GlanceAPITypeSingle)))
		})
		It("is not Ready", func() {
			th.ExpectCondition(
				glanceTest.GlanceSingle,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.ReadyCondition,
				corev1.ConditionFalse,
			)
		})
		It("has empty Status fields", func() {
			instance := GetGlanceAPI(glanceTest.GlanceSingle)
			Expect(instance.Status.Hash).To(BeEmpty())
			Expect(instance.Status.ReadyCount).To(Equal(int32(0)))
		})
	})
	When("an unrelated Secret is created the CR state does not change", func() {
		BeforeEach(func() {
			DeferCleanup(th.DeleteInstance, CreateGlanceAPI(glanceTest.GlanceSingle, GetDefaultGlanceAPISpec(GlanceAPITypeSingle)))
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-relevant-secret",
					Namespace: glanceTest.Instance.Namespace,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, secret)
		})
		It("is not Ready", func() {
			th.ExpectCondition(
				glanceTest.GlanceSingle,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.ReadyCondition,
				corev1.ConditionFalse,
			)
		})
	})
	When("the Secret is created with all the expected fields", func() {
		BeforeEach(func() {
			DeferCleanup(th.DeleteInstance, CreateDefaultGlance(glanceTest.Instance))
			spec := GetDefaultGlanceAPISpec(GlanceAPITypeSingle)
			spec["customServiceConfig"] = "foo=bar"
			DeferCleanup(th.DeleteInstance, CreateGlanceAPI(glanceTest.GlanceSingle, spec))
			DeferCleanup(keystone.DeleteKeystoneAPI, keystone.CreateKeystoneAPI(glanceTest.Instance.Namespace))
		})
		It("reports that input is ready", func() {
			th.ExpectCondition(
				glanceTest.GlanceSingle,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.InputReadyCondition,
				corev1.ConditionTrue,
			)
		})
		It("generated configs successfully", func() {
			th.ExpectCondition(
				glanceTest.GlanceSingle,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.ServiceConfigReadyCondition,
				corev1.ConditionTrue,
			)
			secretDataMap := th.GetSecret(glanceTest.GlanceSingleConfigMapData)
			Expect(secretDataMap).ShouldNot(BeNil())
			// We apply customServiceConfig to the GlanceAPI Pod
			Expect(secretDataMap.Data).Should(HaveKey("02-config.conf"))
			//Double check customServiceConfig has been applied
			configData := string(secretDataMap.Data["02-config.conf"])
			Expect(configData).Should(ContainSubstring("foo=bar"))
		})
		It("stored the input hash in the Status", func() {
			Eventually(func(g Gomega) {
				glanceAPI := GetGlanceAPI(glanceTest.GlanceSingle)
				g.Expect(glanceAPI.Status.Hash).Should(HaveKeyWithValue("input", Not(BeEmpty())))
			}, timeout, interval).Should(Succeed())
		})
	})
	When("GlanceAPI is generated by the top-level CR", func() {
		BeforeEach(func() {
			DeferCleanup(th.DeleteInstance, CreateDefaultGlance(glanceTest.Instance))
			DeferCleanup(th.DeleteInstance, CreateGlanceAPI(glanceTest.GlanceInternal, GetDefaultGlanceAPISpec(GlanceAPITypeInternal)))
			DeferCleanup(th.DeleteInstance, CreateGlanceAPI(glanceTest.GlanceExternal, GetDefaultGlanceAPISpec(GlanceAPITypeExternal)))
			DeferCleanup(keystone.DeleteKeystoneAPI, keystone.CreateKeystoneAPI(glanceTest.Instance.Namespace))
			th.ExpectCondition(
				glanceTest.GlanceInternal,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.ServiceConfigReadyCondition,
				corev1.ConditionTrue,
			)
			th.ExpectCondition(
				glanceTest.GlanceExternal,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.ServiceConfigReadyCondition,
				corev1.ConditionTrue,
			)
		})
		It("creates a Deployment for glance-api service - Internal", func() {
			th.SimulateStatefulSetReplicaReady(glanceTest.GlanceInternalAPI)

			ss := th.GetStatefulSet(glanceTest.GlanceInternalAPI)
			// Check the resulting deployment fields
			Expect(int(*ss.Spec.Replicas)).To(Equal(1))
			Expect(ss.Spec.Template.Spec.Volumes).To(HaveLen(3))
			Expect(ss.Spec.Template.Spec.Containers).To(HaveLen(3))

			container := ss.Spec.Template.Spec.Containers[2]
			Expect(container.VolumeMounts).To(HaveLen(4))
			Expect(container.Image).To(Equal(glanceTest.ContainerImage))
			Expect(container.LivenessProbe.HTTPGet.Port.IntVal).To(Equal(int32(9292)))
			Expect(container.ReadinessProbe.HTTPGet.Port.IntVal).To(Equal(int32(9292)))
		})
		It("creates a Deployment for glance-api service - External", func() {
			th.SimulateStatefulSetReplicaReady(glanceTest.GlanceExternalAPI)
			ss := th.GetStatefulSet(glanceTest.GlanceExternalAPI)
			// Check the resulting deployment fields
			Expect(int(*ss.Spec.Replicas)).To(Equal(1))
			Expect(ss.Spec.Template.Spec.Volumes).To(HaveLen(3))
			Expect(ss.Spec.Template.Spec.Containers).To(HaveLen(3))

			// Check the glance-api container
			container := ss.Spec.Template.Spec.Containers[2]
			Expect(container.VolumeMounts).To(HaveLen(4))
			Expect(container.Image).To(Equal(glanceTest.ContainerImage))
			Expect(container.LivenessProbe.HTTPGet.Port.IntVal).To(Equal(int32(9292)))
			Expect(container.ReadinessProbe.HTTPGet.Port.IntVal).To(Equal(int32(9292)))

			// Check the glance-httpd container
			container = ss.Spec.Template.Spec.Containers[1]
			Expect(container.VolumeMounts).To(HaveLen(2))
			Expect(container.Image).To(Equal(glanceTest.ContainerImage))

			// Check the glance-log container
			container = ss.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(1))
			Expect(container.Image).To(Equal(glanceTest.ContainerImage))
		})
	})
	When("GlanceAPI is generated by the top-level CR (single-api)", func() {
		BeforeEach(func() {
			DeferCleanup(th.DeleteInstance, CreateDefaultGlance(glanceTest.Instance))
			DeferCleanup(th.DeleteInstance, CreateGlanceAPI(glanceTest.GlanceSingle, GetDefaultGlanceAPISpec(GlanceAPITypeSingle)))
			DeferCleanup(keystone.DeleteKeystoneAPI, keystone.CreateKeystoneAPI(glanceTest.Instance.Namespace))
			th.ExpectCondition(
				glanceTest.GlanceSingle,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.ServiceConfigReadyCondition,
				corev1.ConditionTrue,
			)
		})
		It("creates a Deployment for glance-single-api service", func() {
			th.SimulateDeploymentReplicaReady(glanceTest.GlanceSingleAPI)
			ss := th.GetDeployment(glanceTest.GlanceSingleAPI)
			// Check the resulting deployment fields
			Expect(int(*ss.Spec.Replicas)).To(Equal(1))
			Expect(ss.Spec.Template.Spec.Volumes).To(HaveLen(4))
			Expect(ss.Spec.Template.Spec.Containers).To(HaveLen(3))

			container := ss.Spec.Template.Spec.Containers[2]
			Expect(container.VolumeMounts).To(HaveLen(4))
			Expect(container.Image).To(Equal(glanceTest.ContainerImage))
			Expect(container.LivenessProbe.HTTPGet.Port.IntVal).To(Equal(int32(9292)))
			Expect(container.ReadinessProbe.HTTPGet.Port.IntVal).To(Equal(int32(9292)))
		})
	})
	When("the Deployment has at least one Replica ready - External", func() {
		BeforeEach(func() {
			DeferCleanup(th.DeleteInstance, CreateGlanceAPI(glanceTest.GlanceExternal, GetDefaultGlanceAPISpec(GlanceAPITypeExternal)))
			DeferCleanup(keystone.DeleteKeystoneAPI, keystone.CreateKeystoneAPI(glanceTest.GlanceExternal.Namespace))
			th.SimulateStatefulSetReplicaReady(glanceTest.GlanceExternalAPI)
			keystone.SimulateKeystoneEndpointReady(glanceTest.GlanceExternal)
		})
		It("reports that Deployment is ready", func() {
			th.ExpectCondition(
				glanceTest.GlanceExternal,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.DeploymentReadyCondition,
				corev1.ConditionTrue,
			)
			// Deployment is Ready, check the actual ReadyCount is > 0
			glanceAPI := GetGlanceAPI(glanceTest.GlanceExternal)
			Expect(glanceAPI.Status.ReadyCount).To(BeNumerically(">", 0))
		})
		It("exposes the service", func() {
			apiInstance := th.GetService(glanceTest.GlancePublicSvc)
			Expect(apiInstance.Labels["service"]).To(Equal("glance-external"))
		})
		It("creates KeystoneEndpoint", func() {
			keystoneEndpoint := keystone.GetKeystoneEndpoint(glanceTest.GlanceExternal)
			endpoints := keystoneEndpoint.Spec.Endpoints
			Expect(endpoints).To(HaveKeyWithValue("public", "http://glance-public."+glanceTest.Instance.Namespace+".svc:9292"))
			th.ExpectCondition(
				glanceTest.GlanceExternal,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.KeystoneEndpointReadyCondition,
				corev1.ConditionTrue,
			)
		})
	})
	When("the Deployment has at least one Replica ready - Internal", func() {
		BeforeEach(func() {
			DeferCleanup(th.DeleteInstance, CreateGlanceAPI(glanceTest.GlanceInternal, GetDefaultGlanceAPISpec(GlanceAPITypeInternal)))
			DeferCleanup(keystone.DeleteKeystoneAPI, keystone.CreateKeystoneAPI(glanceTest.GlanceInternal.Namespace))
			th.SimulateStatefulSetReplicaReady(glanceTest.GlanceInternalAPI)
			keystone.SimulateKeystoneEndpointReady(glanceTest.GlanceInternalSvc)
		})
		It("reports that Deployment is ready", func() {
			th.ExpectCondition(
				glanceTest.GlanceInternal,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.DeploymentReadyCondition,
				corev1.ConditionTrue,
			)
			// Deployment is Ready, check the actual ReadyCount is > 0
			glanceAPI := GetGlanceAPI(glanceTest.GlanceInternal)
			Expect(glanceAPI.Status.ReadyCount).To(BeNumerically(">", 0))
		})
		It("exposes the service", func() {
			apiInstance := th.GetService(glanceTest.GlanceInternalSvc)
			Expect(apiInstance.Labels["service"]).To(Equal("glance-internal"))
		})
		It("creates KeystoneEndpoint", func() {
			keystoneEndpoint := keystone.GetKeystoneEndpoint(glanceTest.GlanceInternal)
			endpoints := keystoneEndpoint.Spec.Endpoints
			Expect(endpoints).To(HaveKeyWithValue("internal", "http://glance-internal."+glanceTest.Instance.Namespace+".svc:9292"))
			th.ExpectCondition(
				glanceTest.GlanceInternal,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.KeystoneEndpointReadyCondition,
				corev1.ConditionTrue,
			)
		})
	})
	When("the Deployment has at least one Replica ready - Single", func() {
		BeforeEach(func() {
			DeferCleanup(th.DeleteInstance, CreateGlanceAPI(glanceTest.GlanceSingle, GetDefaultGlanceAPISpec(GlanceAPITypeSingle)))
			DeferCleanup(keystone.DeleteKeystoneAPI, keystone.CreateKeystoneAPI(glanceTest.GlanceSingle.Namespace))
			th.SimulateDeploymentReplicaReady(glanceTest.GlanceSingleAPI)
			keystone.SimulateKeystoneEndpointReady(glanceTest.GlanceSingle)
		})
		It("reports that Deployment is ready", func() {
			th.ExpectCondition(
				glanceTest.GlanceSingle,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.DeploymentReadyCondition,
				corev1.ConditionTrue,
			)
			// Deployment is Ready, check the actual ReadyCount is > 0
			glanceAPI := GetGlanceAPI(glanceTest.GlanceSingle)
			Expect(glanceAPI.Status.ReadyCount).To(BeNumerically(">", 0))
		})
		It("exposes the service", func() {
			apiInstance := th.GetService(glanceTest.GlanceInternalSvc)
			Expect(apiInstance.Labels["service"]).To(Equal("glance-single"))
		})
		It("creates KeystoneEndpoint", func() {
			keystoneEndpoint := keystone.GetKeystoneEndpoint(glanceTest.GlanceSingle)
			endpoints := keystoneEndpoint.Spec.Endpoints
			Expect(endpoints).To(HaveKeyWithValue("internal", "http://glance-internal."+glanceTest.Instance.Namespace+".svc:9292"))
			th.ExpectCondition(
				glanceTest.GlanceSingle,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.KeystoneEndpointReadyCondition,
				corev1.ConditionTrue,
			)
		})
	})
	When("A GlanceAPI is created with service override", func() {
		BeforeEach(func() {
			spec := GetDefaultGlanceAPISpec(GlanceAPITypeInternal)
			serviceOverride := map[string]interface{}{}
			serviceOverride["internal"] = map[string]interface{}{
				"endpoint": "internal",
				"metadata": map[string]map[string]string{
					"annotations": {
						"dnsmasq.network.openstack.org/hostname": "glance-internal.openstack.svc",
						"metallb.universe.tf/address-pool":       "osp-internalapi",
						"metallb.universe.tf/allow-shared-ip":    "osp-internalapi",
						"metallb.universe.tf/loadBalancerIPs":    "internal-lb-ip-1,internal-lb-ip-2",
					},
					"labels": {
						"internal": "true",
						"service":  "glance",
					},
				},
				"spec": map[string]interface{}{
					"type": "LoadBalancer",
				},
			}

			spec["override"] = map[string]interface{}{
				"service": serviceOverride,
			}
			glance := CreateGlanceAPI(glanceTest.GlanceInternal, spec)
			DeferCleanup(keystone.DeleteKeystoneAPI, keystone.CreateKeystoneAPI(glanceTest.GlanceInternal.Namespace))
			th.SimulateStatefulSetReplicaReady(glanceTest.GlanceInternalAPI)
			keystone.SimulateKeystoneEndpointReady(glanceTest.GlanceInternal)
			DeferCleanup(th.DeleteInstance, glance)
		})
		It("creates KeystoneEndpoint", func() {
			keystoneEndpoint := keystone.GetKeystoneEndpoint(glanceTest.GlanceInternal)
			endpoints := keystoneEndpoint.Spec.Endpoints
			Expect(endpoints).To(HaveKeyWithValue("internal", "http://glance-internal."+glanceTest.GlanceInternal.Namespace+".svc:9292"))
			th.ExpectCondition(
				glanceTest.GlanceInternal,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.KeystoneEndpointReadyCondition,
				corev1.ConditionTrue,
			)
		})
		It("creates LoadBalancer service", func() {
			// As the internal endpoint is configured in service overrides it
			// gets a LoadBalancer Service with annotations
			service := th.GetService(glanceTest.GlanceInternalSvc)
			Expect(service.Annotations).To(
				HaveKeyWithValue("dnsmasq.network.openstack.org/hostname", "glance-internal.openstack.svc"))
			Expect(service.Annotations).To(
				HaveKeyWithValue("metallb.universe.tf/address-pool", "osp-internalapi"))
			Expect(service.Annotations).To(
				HaveKeyWithValue("metallb.universe.tf/allow-shared-ip", "osp-internalapi"))
			Expect(service.Annotations).To(
				HaveKeyWithValue("metallb.universe.tf/loadBalancerIPs", "internal-lb-ip-1,internal-lb-ip-2"))

			th.ExpectCondition(
				glanceTest.GlanceInternal,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.ReadyCondition,
				corev1.ConditionTrue,
			)
		})
	})
	When("A GlanceAPI is created with service override endpointURL set", func() {
		BeforeEach(func() {
			spec := GetDefaultGlanceAPISpec(GlanceAPITypeExternal)
			serviceOverride := map[string]interface{}{}
			serviceOverride["public"] = map[string]interface{}{
				"endpoint":    "public",
				"endpointURL": "http://glance-openstack.apps-crc.testing",
			}
			spec["override"] = map[string]interface{}{
				"service": serviceOverride,
			}
			glance := CreateGlanceAPI(glanceTest.GlanceExternal, spec)
			DeferCleanup(keystone.DeleteKeystoneAPI, keystone.CreateKeystoneAPI(glanceTest.GlanceExternal.Namespace))
			th.SimulateStatefulSetReplicaReady(glanceTest.GlanceExternalAPI)
			keystone.SimulateKeystoneEndpointReady(glanceTest.GlanceExternal)
			DeferCleanup(th.DeleteInstance, glance)
		})
		It("creates KeystoneEndpoint", func() {
			keystoneEndpoint := keystone.GetKeystoneEndpoint(glanceTest.GlanceExternal)
			endpoints := keystoneEndpoint.Spec.Endpoints
			Expect(endpoints).To(HaveKeyWithValue("public", "http://glance-openstack.apps-crc.testing"))

			th.ExpectCondition(
				glanceTest.GlanceExternal,
				ConditionGetterFunc(GlanceAPIConditionGetter),
				condition.KeystoneEndpointReadyCondition,
				corev1.ConditionTrue,
			)
		})
	})
})
