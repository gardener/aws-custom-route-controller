/*
 * SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
 *
 * SPDX-License-Identifier: Apache-2.0
 */

package updater

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
)

// NodeRoute stores node internal IP and the pod CIDRs
type NodeRoute struct {
	InternalIP string
	PodCIDR    string
}

type NodeRoutesUpdater func(routes []NodeRoute)

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
	if r.routes[node.Name] != *route {
		r.routes[node.Name] = *route
		changed = true
		r.changed = true
	}
	return route, changed
}

func (r *NamedNodeRoutes) RemoveNodeRoute(nodeName string) bool {
	r.Lock()
	defer r.Unlock()

	if _, ok := r.routes[nodeName]; ok {
		delete(r.routes, nodeName)
		r.changed = true
		return true
	}

	return false
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

// extractNodeRoute extracts node internal IP and the pod CIDRs
func extractNodeRoute(node *corev1.Node) *NodeRoute {
	if node == nil {
		return nil
	}
	internalIP := ""
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			internalIP = address.Address
			break
		}
	}
	if internalIP == "" || node.Spec.PodCIDR == "" {
		return nil
	}
	return &NodeRoute{
		InternalIP: internalIP,
		PodCIDR:    node.Spec.PodCIDR,
	}
}
