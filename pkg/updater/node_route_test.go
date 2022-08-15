// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package updater_test

import (
	"github.com/gardener/aws-custom-route-controller/pkg/updater"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("NamedNodeRoutes", func() {
	var (
		podCIDR1        = "10.0.1.0/24"
		node1InstanceID = "i-0001"
		node1           = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
			},
			Spec: corev1.NodeSpec{
				PodCIDR:    podCIDR1,
				ProviderID: makeProviderID(node1InstanceID),
			},
		}
		podCIDR2        = "10.0.7.0/24"
		node2InstanceID = "i-0001"
		node2           = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node2",
			},
			Spec: corev1.NodeSpec{
				PodCIDR:    podCIDR2,
				ProviderID: makeProviderID(node2InstanceID),
			},
		}
		podCIDR3 = "10.0.33.0/24"
		node3    = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node3",
			},
			Spec: corev1.NodeSpec{
				PodCIDR: podCIDR3,
			},
		}
	)

	It("should extract node data", func() {
		routes := updater.NewNamedNodeRoutes()
		route1, changed1 := routes.AddNodeRoute(node1)
		Expect(route1).To(Equal(updater.NewNodeRoute(node1InstanceID, podCIDR1)))
		Expect(changed1).To(BeTrue())
		route1b, changed1b := routes.AddNodeRoute(node1)
		Expect(route1b).NotTo(BeNil())
		Expect(changed1b).To(BeFalse())

		route2, changed2 := routes.AddNodeRoute(node2)
		Expect(route2).To(Equal(updater.NewNodeRoute(node2InstanceID, podCIDR2)))
		Expect(changed2).To(BeTrue())

		route3, changed3 := routes.AddNodeRoute(node3)
		Expect(route3).To(BeNil())
		Expect(changed3).To(BeFalse())

		routes1 := routes.GetRoutesIfChanged()
		Expect(len(routes1)).To(Equal(2))
		routes1b := routes.GetRoutesIfChanged()
		Expect(routes1b).To(BeNil())

		route1c, changed1c := routes.AddNodeRoute(node1)
		Expect(route1c).NotTo(BeNil())
		Expect(changed1c).To(BeFalse())

		changed3b := routes.RemoveNodeRoute(node3.Name)
		Expect(changed3b).To(BeFalse())
		changed2b := routes.RemoveNodeRoute(node2.Name)
		Expect(changed2b).To(BeTrue())

		routes2 := routes.GetRoutesIfChanged()
		Expect(len(routes2)).To(Equal(1))
	})
})

func makeProviderID(instanceID string) string {
	return "aws:///eu-west-1a/" + instanceID
}
