/*
 * SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
 *
 * SPDX-License-Identifier: Apache-2.0
 */

package updater

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"

	"github.com/gardener/aws-custom-route-controller/pkg/util"
)

// NodeRoute stores node internal IP and the pod CIDRs
type NodeRoute struct {
	InstanceID string
	PodCIDR    string
}

func NewNodeRoute(instanceID, podCIDR string) *NodeRoute {
	if instanceID == "" || podCIDR == "" {
		return nil
	}

	nodeRoute := &NodeRoute{
		InstanceID: instanceID,
		PodCIDR:    podCIDR,
	}
	if _, _, err := net.ParseCIDR(podCIDR); err != nil {
		return nil
	}
	return nodeRoute
}

func (r NodeRoute) Equals(other *NodeRoute) bool {
	if other == nil {
		return false
	}
	return r == *other
}

type NodeRoutesUpdater func(ctx context.Context, routes []NodeRoute, tick func()) (*RouteUpdateResult, error)

type NamedNodeRoutes struct {
	sync.Mutex
	routes  map[string]NodeRoute
	changed bool
}

func NewNamedNodeRoutes() *NamedNodeRoutes {
	return &NamedNodeRoutes{
		routes: map[string]NodeRoute{},
	}
}

func (r *NamedNodeRoutes) AddNodeRoute(node *corev1.Node) (*NodeRoute, bool) {
	route := extractNodeRoute(node)
	if route == nil {
		return nil, false
	}

	r.Lock()
	defer r.Unlock()

	changed := false
	if !r.routes[node.Name].Equals(route) {
		r.routes[node.Name] = *route
		changed = true
		r.changed = true
	}
	return route, changed
}

func (r *NamedNodeRoutes) RemoveNodeRoute(nodeName string) *NodeRoute {
	r.Lock()
	defer r.Unlock()

	if nr, ok := r.routes[nodeName]; ok {
		delete(r.routes, nodeName)
		r.changed = true
		return &nr
	}

	return nil
}

func (r *NamedNodeRoutes) GetRoutesIfChanged() []NodeRoute {
	r.Lock()
	defer r.Unlock()
	if !r.changed {
		return nil
	}
	var routes []NodeRoute
	for _, route := range r.routes {
		routes = append(routes, route)
	}
	r.changed = false
	return routes
}

func (r *NamedNodeRoutes) SetChanged() {
	r.Lock()
	defer r.Unlock()
	r.changed = true
}

// extractNodeRoute extracts node internal IP and the pod CIDRs
func extractNodeRoute(node *corev1.Node) *NodeRoute {
	if node == nil {
		return nil
	}
	_, instanceID, _ := decodeRegionAndInstanceID(node.Spec.ProviderID)
	podCIDR, _ := util.GetIPv4CIDR(node.Spec.PodCIDRs)
	return NewNodeRoute(instanceID, podCIDR)
}

// decodeRegionAndInstanceID extracts region and instanceID
func decodeRegionAndInstanceID(providerID string) (string, string, error) {
	if !strings.HasPrefix(providerID, "aws:") {
		err := fmt.Errorf("unknown scheme, expected 'aws': %s", providerID)
		return "", "", err
	}
	splitProviderID := strings.Split(providerID, "/")
	if len(splitProviderID) < 2 {
		err := fmt.Errorf("unable to decode provider-ID")
		return "", "", err
	}
	return splitProviderID[len(splitProviderID)-2], splitProviderID[len(splitProviderID)-1], nil
}
