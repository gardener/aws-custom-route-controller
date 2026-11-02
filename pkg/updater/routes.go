/*
 * SPDX-FileCopyrightText: Contributors to the Gardener project
 *
 * SPDX-License-Identifier: Apache-2.0
 */

package updater

import (
	"context"
	"fmt"
	"net"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/go-logr/logr"
	"go.uber.org/multierr"
)

// RouteUpdateResult tracks the result of updating routes for each node
type RouteUpdateResult struct {
	SuccessfulRoutes map[string]bool // maps pod CIDR to success status
}

// CustomRoutes updates route tables for an AWS cluster
type CustomRoutes struct {
	log         logr.Logger
	ec2         EC2Routes
	clusterName string
	podNetwork  net.IPNet
}

// NewCustomRoutes creates a new CustomRoutes instance
func NewCustomRoutes(log logr.Logger, ec2Routes EC2Routes, clusterName, podNetworkCIDR string) (*CustomRoutes, error) {
	_, ipnet, err := net.ParseCIDR(podNetworkCIDR)
	if err != nil {
		return nil, err
	}
	return &CustomRoutes{
		log:         log,
		ec2:         ec2Routes,
		clusterName: clusterName,
		podNetwork:  *ipnet,
	}, nil
}

type internalNodeRoute struct {
	destinationCidrBlock string
	instanceId           string
}

func (r *CustomRoutes) findRouteTables(ctx context.Context) ([]ec2types.RouteTable, error) {
	var tables []ec2types.RouteTable

	request := &ec2.DescribeRouteTablesInput{}
	response, err := r.ec2.DescribeRouteTables(ctx, request)
	if err != nil {
		return nil, err
	}

	for _, table := range response.RouteTables {
		if hasClusterTag(r.clusterName, table.Tags) {
			tables = append(tables, table)
		}
	}

	if len(tables) == 0 {
		return nil, fmt.Errorf("unable to find route table for AWS cluster: %s", r.clusterName)
	}

	return tables, nil
}

func (r *CustomRoutes) getNetworkInterfaces(ctx context.Context, instanceID string) ([]string, error) {
	request := &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	}
	response, err := r.ec2.DescribeInstances(ctx, request)
	if err != nil {
		return nil, err
	}

	if len(response.Reservations) == 0 || len(response.Reservations[0].Instances) == 0 {
		return nil, fmt.Errorf("instance %s not found", instanceID)
	}

	instance := response.Reservations[0].Instances[0]

	// Sort network interfaces by device index so primary ENI (device 0) is first
	enis := instance.NetworkInterfaces
	sort.Slice(enis, func(i, j int) bool {
		di := int32(0)
		dj := int32(0)
		if enis[i].Attachment != nil && enis[i].Attachment.DeviceIndex != nil {
			di = *enis[i].Attachment.DeviceIndex
		}
		if enis[j].Attachment != nil && enis[j].Attachment.DeviceIndex != nil {
			dj = *enis[j].Attachment.DeviceIndex
		}
		return di < dj
	})

	var networkInterfaceIds []string
	for _, eni := range enis {
		if eni.NetworkInterfaceId != nil {
			networkInterfaceIds = append(networkInterfaceIds, *eni.NetworkInterfaceId)
		}
	}

	return networkInterfaceIds, nil
}

// Update updates all found route tables (tagged with the clusterName) with the podCIDR to node instance routes
// Returns a RouteUpdateResult that tracks which routes were successfully created
func (r *CustomRoutes) Update(ctx context.Context, routes []NodeRoute, tick func()) (*RouteUpdateResult, error) {
	tick()
	tables, err := r.findRouteTables(ctx)
	if err != nil {
		// No route work was attempted, so report no per-route outcomes: a fabricated
		// all-false result would mark every node NetworkUnavailable on a transient failure.
		return nil, err
	}

	result := &RouteUpdateResult{
		SuccessfulRoutes: make(map[string]bool),
	}

	// A route is successful only if present in every managed table, so outcomes are
	// AND-reduced across tables: start optimistic and let any table flip a CIDR to false.
	for _, route := range routes {
		result.SuccessfulRoutes[route.PodCIDR] = true
	}

	var updateErrors error
	for _, table := range tables {
		tick()
		toBeCreated, toBeDeleted, alreadyPresent := r.calcRouteChanges(table, routes)

		// The main table carries no node pod routes, so it has no say in reachability; skip
		// its vote but still run its deletions.
		if r.isMainTable(table) {
			if len(toBeDeleted) == 0 && len(toBeCreated) == 0 {
				r.log.Info("no routes updated", "table", *table.RouteTableId)
			}
			r.deleteRoutes(ctx, table, toBeDeleted, tick, &updateErrors)
			continue
		}

		r.deleteRoutes(ctx, table, toBeDeleted, tick, &updateErrors)

		// Seed with routes already present in this table (an explicit result from
		// calcRouteChanges, not inferred from absence in toBeCreated).
		tableSuccess := make(map[string]bool, len(routes))
		for _, cidr := range alreadyPresent {
			tableSuccess[cidr] = true
		}

		for _, create := range toBeCreated {
			networkInterfaceIds, err := r.getNetworkInterfaces(ctx, create.instanceId)
			if err != nil {
				updateErrors = multierr.Append(updateErrors, fmt.Errorf("getting network interfaces for instance %s failed: %w", create.instanceId, err))
				tableSuccess[create.destinationCidrBlock] = false
				continue
			}

			// Multi-NIC instances: AWS rejects InstanceId in CreateRoute, must use NetworkInterfaceId.
			// Try each ENI (sorted by device index), first success wins.
			if len(networkInterfaceIds) > 1 {
				routeCreated := false
				for _, eniId := range networkInterfaceIds {
					req := &ec2.CreateRouteInput{
						DestinationCidrBlock: aws.String(create.destinationCidrBlock),
						NetworkInterfaceId:   aws.String(eniId),
						RouteTableId:         table.RouteTableId,
					}
					tick()
					_, err = r.ec2.CreateRoute(ctx, req)
					if err == nil {
						r.log.Info("route created", "table", *table.RouteTableId, "destination", create.destinationCidrBlock, "instanceId", create.instanceId, "networkInterfaceId", eniId)
						routeCreated = true
						break
					}
				}
				if !routeCreated {
					updateErrors = multierr.Append(updateErrors, fmt.Errorf("creating route %s -> %s in table %s failed on all ENIs", create.destinationCidrBlock, create.instanceId, *table.RouteTableId))
				}
				tableSuccess[create.destinationCidrBlock] = routeCreated
			} else {
				// Single NIC: use InstanceId as before
				req := &ec2.CreateRouteInput{
					DestinationCidrBlock: aws.String(create.destinationCidrBlock),
					InstanceId:           aws.String(create.instanceId),
					RouteTableId:         table.RouteTableId,
				}
				tick()
				_, err = r.ec2.CreateRoute(ctx, req)
				if err != nil {
					updateErrors = multierr.Append(updateErrors, fmt.Errorf("creating route %s -> %s in table %s failed: %w", create.destinationCidrBlock, create.instanceId, *table.RouteTableId, err))
					tableSuccess[create.destinationCidrBlock] = false
					continue
				}
				tableSuccess[create.destinationCidrBlock] = true
				r.log.Info("route created", "table", *table.RouteTableId, "destination", create.destinationCidrBlock, "instanceId", create.instanceId)
			}
		}

		// AND this table's outcome into the running result: a CIDR not carried by this table
		// makes the node not network-available.
		for _, route := range routes {
			if !tableSuccess[route.PodCIDR] {
				result.SuccessfulRoutes[route.PodCIDR] = false
			}
		}

		if len(toBeDeleted) == 0 && len(toBeCreated) == 0 {
			r.log.Info("no routes updated", "table", *table.RouteTableId)
		}
	}

	return result, updateErrors
}

func (r *CustomRoutes) deleteRoutes(ctx context.Context, table ec2types.RouteTable, toBeDeleted []internalNodeRoute, tick func(), updateErrors *error) {
	for _, del := range toBeDeleted {
		req := &ec2.DeleteRouteInput{
			RouteTableId:         table.RouteTableId,
			DestinationCidrBlock: aws.String(del.destinationCidrBlock),
		}
		tick()
		if _, err := r.ec2.DeleteRoute(ctx, req); err != nil {
			*updateErrors = multierr.Append(*updateErrors, fmt.Errorf("deleting route %s in table %s failed: %w", del.destinationCidrBlock, *table.RouteTableId, err))
			continue
		}
		r.log.Info("route deleted", "table", *table.RouteTableId, "destination", del.destinationCidrBlock, "instanceId", del.instanceId)
	}
}

func (r *CustomRoutes) isMainTable(table ec2types.RouteTable) bool {
	return getNameTagValue(table.Tags) == r.clusterName
}

func (r *CustomRoutes) calcRouteChanges(table ec2types.RouteTable, nodeRoutes []NodeRoute) (toBeCreated, toBeDeleted []internalNodeRoute, alreadyPresent []string) {
	if r.isMainTable(table) {
		nodeRoutes = nil
	}
	found := make([]bool, len(nodeRoutes))
outer:
	for _, route := range table.Routes {
		if route.Origin != ec2types.RouteOriginCreateRoute {
			continue
		}
		if route.DestinationCidrBlock == nil {
			continue
		}
		if _, ipnet, err := net.ParseCIDR(*route.DestinationCidrBlock); err != nil || !r.podNetwork.Contains(ipnet.IP) {
			continue
		}
		for i, nr := range nodeRoutes {
			if nr.PodCIDR == *route.DestinationCidrBlock && route.InstanceId != nil && nr.InstanceID == *route.InstanceId {
				found[i] = true
				alreadyPresent = append(alreadyPresent, nr.PodCIDR)
				continue outer
			}
		}
		toBeDeleted = append(toBeDeleted, internalNodeRoute{
			destinationCidrBlock: *route.DestinationCidrBlock,
		})
	}

	for i, nr := range nodeRoutes {
		if found[i] {
			continue
		}
		if !r.podNetworkContains(nr.PodCIDR) {
			// Node-supplied destinations outside the pod network must never be written: they
			// also escape the delete-side filter, so such a route could never be removed.
			r.log.Info("skipping route creation: node pod CIDR outside configured pod network",
				"podCIDR", nr.PodCIDR, "instanceId", nr.InstanceID, "podNetwork", r.podNetwork.String())
			continue
		}
		route := internalNodeRoute{
			destinationCidrBlock: nr.PodCIDR,
			instanceId:           nr.InstanceID,
		}
		toBeCreated = append(toBeCreated, route)
	}

	return
}

// podNetworkContains reports whether cidr is fully within the configured pod network: its
// network address must be inside podNetwork and its prefix no wider, so a CIDR whose base
// passes but whose range spills out is rejected.
func (r *CustomRoutes) podNetworkContains(cidr string) bool {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	if !r.podNetwork.Contains(ipnet.IP) {
		return false
	}
	nodeOnes, nodeBits := ipnet.Mask.Size()
	podOnes, podBits := r.podNetwork.Mask.Size()
	if nodeBits != podBits {
		// Different address families.
		return false
	}
	return nodeOnes >= podOnes
}
