// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package updater_test

import (
	"github.com/gardener/aws-custom-route-controller/pkg/updater"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("NamedNodeRoutes", func() {
	var (
		podCIDRs1       = []string{"10.0.1.0/24"}
		node1InstanceID = "i-0001"
		node1           = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
			},
			Spec: corev1.NodeSpec{
				PodCIDRs:   podCIDRs1,
				ProviderID: makeProviderID(node1InstanceID),
			},
		}
		podCIDRs2       = []string{"10.0.7.0/24"}
		node2InstanceID = "i-0001"
		node2           = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node2",
			},
			Spec: corev1.NodeSpec{
				PodCIDRs:   podCIDRs2,
				ProviderID: makeProviderID(node2InstanceID),
			},
		}
		podCIDRs3 = []string{"10.0.33.0/24"}
		node3     = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node3",
			},
			Spec: corev1.NodeSpec{
				PodCIDRs: podCIDRs3,
			},
		}
	)

	It("should extract node data", func() {
		routes := updater.NewNamedNodeRoutes(logf.Log.WithName("test"))
		route1, changed1 := routes.AddNodeRoute(node1)
		Expect(route1).To(Equal(updater.NewNodeRoute(node1InstanceID, podCIDRs1[0])))
		Expect(changed1).To(BeTrue())
		route1b, changed1b := routes.AddNodeRoute(node1)
		Expect(route1b).NotTo(BeNil())
		Expect(changed1b).To(BeFalse())

		route2, changed2 := routes.AddNodeRoute(node2)
		Expect(route2).To(Equal(updater.NewNodeRoute(node2InstanceID, podCIDRs2[0])))
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
		Expect(changed3b).To(BeNil())
		changed2b := routes.RemoveNodeRoute(node2.Name)
		Expect(changed2b).NotTo(BeNil())
		changed2c := routes.RemoveNodeRoute(node2.Name)
		Expect(changed2c).To(BeNil())

		routes2 := routes.GetRoutesIfChanged()
		Expect(len(routes2)).To(Equal(1))
	})

	It("should store the pod CIDR in canonical network form", func() {
		// A node spec may carry host bits in the pod CIDR; AWS reports routes in canonical
		// form, so the stored value must be canonicalized or it would never match the
		// existing route and would churn delete/recreate every sync.
		route := updater.NewNodeRoute("i-0001", "10.0.1.7/24")
		Expect(route).NotTo(BeNil())
		Expect(route.PodCIDR).To(Equal("10.0.1.0/24"))
	})

	It("should refuse a pod CIDR already claimed by another node and keep the first owner", func() {
		// Without arbitration a re-registering node could claim another node's pod CIDR and
		// win the AWS route. The first node to claim a CIDR keeps it; a later conflicting
		// claim from a different node is dropped.
		routes := updater.NewNamedNodeRoutes(logf.Log.WithName("test"))
		victim := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "victim"},
			Spec: corev1.NodeSpec{
				PodCIDRs:   []string{"10.0.5.0/24"},
				ProviderID: makeProviderID("i-victim"),
			},
		}
		attacker := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "attacker"},
			Spec: corev1.NodeSpec{
				PodCIDRs:   []string{"10.0.5.0/24"}, // same CIDR, different instance
				ProviderID: makeProviderID("i-attacker"),
			},
		}

		victimRoute, victimChanged := routes.AddNodeRoute(victim)
		Expect(victimRoute).NotTo(BeNil())
		Expect(victimChanged).To(BeTrue())

		attackerRoute, attackerChanged := routes.AddNodeRoute(attacker)
		Expect(attackerRoute).To(BeNil())
		Expect(attackerChanged).To(BeFalse())

		stored := routes.GetRoutesIfChanged()
		Expect(len(stored)).To(Equal(1))
		Expect(stored[0].InstanceID).To(Equal("i-victim"))
		Expect(stored[0].PodCIDR).To(Equal("10.0.5.0/24"))
	})

	It("should allow the same node to keep updating its own CIDR", func() {
		// The conflict check is keyed on a DIFFERENT node name owning the CIDR, so a node
		// re-reporting its own CIDR is not treated as a conflict.
		routes := updater.NewNamedNodeRoutes(logf.Log.WithName("test"))
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node"},
			Spec: corev1.NodeSpec{
				PodCIDRs:   []string{"10.0.8.0/24"},
				ProviderID: makeProviderID("i-node"),
			},
		}
		route1, changed1 := routes.AddNodeRoute(node)
		Expect(route1).NotTo(BeNil())
		Expect(changed1).To(BeTrue())
		route2, changed2 := routes.AddNodeRoute(node)
		Expect(route2).NotTo(BeNil())
		Expect(changed2).To(BeFalse())
	})
})

func makeProviderID(instanceID string) string {
	return "aws:///eu-west-1a/" + instanceID
}
