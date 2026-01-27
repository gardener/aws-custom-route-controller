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

// Update updates all found route tables (tagged with the clusterName) with the podCIDR to node instance routes
// Returns a RouteUpdateResult that tracks which routes were successfully created
func (r *CustomRoutes) Update(ctx context.Context, routes []NodeRoute, tick func()) (*RouteUpdateResult, error) {
	result := &RouteUpdateResult{
		SuccessfulRoutes: make(map[string]bool),
	}

	// Initially mark all routes as not successful
	for _, route := range routes {
		result.SuccessfulRoutes[route.PodCIDR] = false
	}

	tick()
	tables, err := r.findRouteTables(ctx)
	if err != nil {
		return result, err
	}

	var updateErrors error
	for _, table := range tables {
		tick()
		toBeCreated, toBeDeleted := r.calcRouteChanges(table, routes)

		for _, del := range toBeDeleted {
			req := &ec2.DeleteRouteInput{
				RouteTableId:         table.RouteTableId,
				DestinationCidrBlock: aws.String(del.destinationCidrBlock),
			}
			tick()
			_, err = r.ec2.DeleteRoute(ctx, req)
			if err != nil {
				updateErrors = multierr.Append(updateErrors, fmt.Errorf("deleting route %s in table %s failed: %w", del.destinationCidrBlock, *table.RouteTableId, err))
				continue
			}
			r.log.Info("route deleted", "table", *table.RouteTableId, "destination", del.destinationCidrBlock, "instanceId", del.instanceId)
		}

		for _, create := range toBeCreated {
			req := &ec2.CreateRouteInput{
				DestinationCidrBlock: aws.String(create.destinationCidrBlock),
				InstanceId:           aws.String(create.instanceId),
				RouteTableId:         table.RouteTableId,
			}
			tick()
			_, err = r.ec2.CreateRoute(ctx, req)
			if err != nil {
				updateErrors = multierr.Append(updateErrors, fmt.Errorf("creating route %s -> %s in table %s failed: %w", create.destinationCidrBlock, create.instanceId, *table.RouteTableId, err))
				result.SuccessfulRoutes[create.destinationCidrBlock] = false
				continue
			}
			result.SuccessfulRoutes[create.destinationCidrBlock] = true
			r.log.Info("route created", "table", *table.RouteTableId, "destination", create.destinationCidrBlock, "instanceId", create.instanceId)
		}

		// Mark routes that already exist (not in toBeCreated) as successful
		for _, route := range routes {
			foundInToBeCreated := false
			for _, create := range toBeCreated {
				if create.destinationCidrBlock == route.PodCIDR {
					foundInToBeCreated = true
					break
				}
			}
			if !foundInToBeCreated {
				// Route already exists, mark as successful
				result.SuccessfulRoutes[route.PodCIDR] = true
			}
		}

		if len(toBeDeleted) == 0 && len(toBeCreated) == 0 {
			r.log.Info("no routes updated", "table", *table.RouteTableId)
		}
	}

	return result, updateErrors
}

func (r *CustomRoutes) isMainTable(table ec2types.RouteTable) bool {
	return getNameTagValue(table.Tags) == r.clusterName
}

func (r *CustomRoutes) calcRouteChanges(table ec2types.RouteTable, nodeRoutes []NodeRoute) (toBeCreated, toBeDeleted []internalNodeRoute) {
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
		route := internalNodeRoute{
			destinationCidrBlock: nr.PodCIDR,
			instanceId:           nr.InstanceID,
		}
		toBeCreated = append(toBeCreated, route)
	}

	return
}
