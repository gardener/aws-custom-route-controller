// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package updater_test

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/gardener/aws-custom-route-controller/pkg/updater"
)

var _ = Describe("CustomRoutes", func() {
	var (
		ctrl          *gomock.Controller
		customRoutes  *updater.CustomRoutes
		ec2RoutesMock *updater.MockEC2Routes
		ctx           = context.Background()
		clusterName   = "shoot--foo--bar"
		clusterTag    = ec2types.Tag{
			Key:   aws.String(updater.ClusterTagKey(clusterName)),
			Value: aws.String("1"),
		}
		route1 = ec2types.Route{
			DestinationCidrBlock: aws.String("0.0.0.0/0"),
			NatGatewayId:         aws.String("nat123"),
			Origin:               ec2types.RouteOriginCreateRouteTable,
		}
		route2 = ec2types.Route{
			DestinationCidrBlock: aws.String("10.243.222.0/24"),
			InstanceId:           aws.String("i-another"),
			Origin:               ec2types.RouteOriginCreateRoute,
		}
		routeNode1 = ec2types.Route{
			DestinationCidrBlock: aws.String("10.243.3.0/24"),
			InstanceId:           aws.String("i-node1"),
			Origin:               ec2types.RouteOriginCreateRoute,
		}
		routeNode2 = ec2types.Route{
			DestinationCidrBlock: aws.String("10.243.9.0/24"),
			InstanceId:           aws.String("i-node2"),
			Origin:               ec2types.RouteOriginCreateRoute,
		}
		routeNode3 = ec2types.Route{
			DestinationCidrBlock: aws.String("10.243.13.0/24"),
			InstanceId:           aws.String("i-node3"),
			Origin:               ec2types.RouteOriginCreateRoute,
		}
		rt1    = aws.String("rt1")
		rt2    = aws.String("rt2")
		rt3    = aws.String("rt3")
		tables = []ec2types.RouteTable{
			{
				RouteTableId: rt1,
				Tags:         []ec2types.Tag{clusterTag},
				Routes: []ec2types.Route{
					route1,
					route2,
					routeNode1,
					routeNode2,
				},
			},
			{
				RouteTableId: rt2,
				Tags:         []ec2types.Tag{clusterTag},
				Routes: []ec2types.Route{
					route1,
					route2,
				},
			},
			{
				RouteTableId: rt3,
				Routes: []ec2types.Route{
					routeNode1,
				},
			},
		}
		tables2 = []ec2types.RouteTable{
			{
				RouteTableId: rt1,
				Tags:         []ec2types.Tag{clusterTag},
				Routes: []ec2types.Route{
					route1,
					route2,
					routeNode1,
					routeNode3,
				},
			},
		}
		nodeRoutes = []updater.NodeRoute{
			{
				InstanceID: *routeNode1.InstanceId,
				PodCIDR:    *routeNode1.DestinationCidrBlock,
			},
			{
				InstanceID: *routeNode3.InstanceId,
				PodCIDR:    *routeNode3.DestinationCidrBlock,
			},
		}
	)

	logf.SetLogger(zap.New())

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		ec2RoutesMock = updater.NewMockEC2Routes(ctrl)

		var err error
		customRoutes, err = updater.NewCustomRoutes(logf.Log.WithName("test"), ec2RoutesMock, clusterName, "10.243.0.0/19")
		Expect(err).To(BeNil())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("should report error if no route tables found", func() {
		ec2RoutesMock.EXPECT().DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{}).Return(&ec2.DescribeRouteTablesOutput{}, nil)
		_, err := customRoutes.Update(ctx, nil, func() {})
		Expect(err).NotTo(BeNil())
	})

	It("should update route tables", func() {
		ec2RoutesMock.EXPECT().DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{}).Return(&ec2.DescribeRouteTablesOutput{RouteTables: tables}, nil)
		ec2RoutesMock.EXPECT().DeleteRoute(ctx, &ec2.DeleteRouteInput{
			DestinationCidrBlock: routeNode2.DestinationCidrBlock,
			RouteTableId:         rt1,
		})
		// Mock DescribeInstances for node3 on rt1
		ec2RoutesMock.EXPECT().DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: []string{nodeRoutes[1].InstanceID},
		}).Return(&ec2.DescribeInstancesOutput{
			Reservations: []ec2types.Reservation{
				{
					Instances: []ec2types.Instance{
						{
							InstanceId: aws.String(nodeRoutes[1].InstanceID),
							NetworkInterfaces: []ec2types.InstanceNetworkInterface{
								{NetworkInterfaceId: aws.String("eni-node3")},
							},
						},
					},
				},
			},
		}, nil)
		ec2RoutesMock.EXPECT().CreateRoute(ctx, &ec2.CreateRouteInput{
			DestinationCidrBlock: aws.String(nodeRoutes[1].PodCIDR),
			InstanceId:           aws.String(nodeRoutes[1].InstanceID),
			RouteTableId:         rt1,
		})
		// Mock DescribeInstances for node1 on rt2
		ec2RoutesMock.EXPECT().DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: []string{nodeRoutes[0].InstanceID},
		}).Return(&ec2.DescribeInstancesOutput{
			Reservations: []ec2types.Reservation{
				{
					Instances: []ec2types.Instance{
						{
							InstanceId: aws.String(nodeRoutes[0].InstanceID),
							NetworkInterfaces: []ec2types.InstanceNetworkInterface{
								{NetworkInterfaceId: aws.String("eni-node1")},
							},
						},
					},
				},
			},
		}, nil)
		ec2RoutesMock.EXPECT().CreateRoute(ctx, &ec2.CreateRouteInput{
			DestinationCidrBlock: aws.String(nodeRoutes[0].PodCIDR),
			InstanceId:           aws.String(nodeRoutes[0].InstanceID),
			RouteTableId:         rt2,
		})
		// Mock DescribeInstances for node3 on rt2
		ec2RoutesMock.EXPECT().DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: []string{nodeRoutes[1].InstanceID},
		}).Return(&ec2.DescribeInstancesOutput{
			Reservations: []ec2types.Reservation{
				{
					Instances: []ec2types.Instance{
						{
							InstanceId: aws.String(nodeRoutes[1].InstanceID),
							NetworkInterfaces: []ec2types.InstanceNetworkInterface{
								{NetworkInterfaceId: aws.String("eni-node3")},
							},
						},
					},
				},
			},
		}, nil)
		ec2RoutesMock.EXPECT().CreateRoute(ctx, &ec2.CreateRouteInput{
			DestinationCidrBlock: aws.String(nodeRoutes[1].PodCIDR),
			InstanceId:           aws.String(nodeRoutes[1].InstanceID),
			RouteTableId:         rt2,
		})
		result, err := customRoutes.Update(ctx, nodeRoutes, func() {})
		Expect(err).To(BeNil())
		Expect(result).NotTo(BeNil())
		Expect(result.SuccessfulRoutes[nodeRoutes[0].PodCIDR]).To(BeTrue())
		Expect(result.SuccessfulRoutes[nodeRoutes[1].PodCIDR]).To(BeTrue())
	})

	It("should update nothing if unchanged", func() {
		ec2RoutesMock.EXPECT().DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{}).Return(&ec2.DescribeRouteTablesOutput{RouteTables: tables2}, nil)
		result, err := customRoutes.Update(ctx, nodeRoutes, func() {})
		Expect(err).To(BeNil())
		Expect(result).NotTo(BeNil())
		Expect(result.SuccessfulRoutes[nodeRoutes[0].PodCIDR]).To(BeTrue())
		Expect(result.SuccessfulRoutes[nodeRoutes[1].PodCIDR]).To(BeTrue())
	})

	It("should report a route as unsuccessful if it fails in any table, even when a later table already has it", func() {
		// A node is network-available only if its route is present in EVERY managed table.
		// Here the route creation fails in the first table but the route already exists in a
		// second table; the per-table outcomes must be combined with a logical AND so the
		// later success does not mask the earlier failure and wrongly mark the node schedulable.
		singleNode := []updater.NodeRoute{
			{InstanceID: "i-node1", PodCIDR: "10.243.3.0/24"},
		}
		twoTables := []ec2types.RouteTable{
			{
				RouteTableId: rt1,
				Tags:         []ec2types.Tag{clusterTag},
				Routes:       []ec2types.Route{route1}, // route missing here -> must be created
			},
			{
				RouteTableId: rt2,
				Tags:         []ec2types.Tag{clusterTag},
				Routes:       []ec2types.Route{route1, routeNode1}, // already present here
			},
		}
		ec2RoutesMock.EXPECT().DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{}).Return(&ec2.DescribeRouteTablesOutput{RouteTables: twoTables}, nil)
		ec2RoutesMock.EXPECT().DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: []string{"i-node1"},
		}).Return(&ec2.DescribeInstancesOutput{
			Reservations: []ec2types.Reservation{
				{
					Instances: []ec2types.Instance{
						{
							InstanceId: aws.String("i-node1"),
							NetworkInterfaces: []ec2types.InstanceNetworkInterface{
								{NetworkInterfaceId: aws.String("eni-node1")},
							},
						},
					},
				},
			},
		}, nil)
		// Route creation in rt1 fails.
		ec2RoutesMock.EXPECT().CreateRoute(ctx, &ec2.CreateRouteInput{
			DestinationCidrBlock: aws.String("10.243.3.0/24"),
			InstanceId:           aws.String("i-node1"),
			RouteTableId:         rt1,
		}).Return(nil, fmt.Errorf("RouteLimitExceeded"))
		result, err := customRoutes.Update(ctx, singleNode, func() {})
		Expect(err).NotTo(BeNil())
		Expect(result).NotTo(BeNil())
		Expect(result.SuccessfulRoutes["10.243.3.0/24"]).To(BeFalse())
	})

	It("should never create a route for a node pod CIDR outside the configured pod network", func() {
		// A shoot-writable Node spec must not be able to make the seed-credentialed
		// controller write an out-of-network route (e.g. shadowing the default route or
		// a co-tenant node subnet). Such destinations also lie outside the delete-side
		// containment filter, so the planted route would otherwise be permanent.
		maliciousRoutes := []updater.NodeRoute{
			{InstanceID: "i-attacker", PodCIDR: "0.0.0.0/1"},     // shadow default route
			{InstanceID: "i-attacker", PodCIDR: "10.250.0.0/17"}, // co-tenant node subnet
			{InstanceID: "i-attacker", PodCIDR: "10.243.0.0/8"},  // base inside pod net, prefix too wide
		}
		emptyTables := []ec2types.RouteTable{
			{
				RouteTableId: rt1,
				Tags:         []ec2types.Tag{clusterTag},
				Routes:       []ec2types.Route{route1},
			},
		}
		ec2RoutesMock.EXPECT().DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{}).Return(&ec2.DescribeRouteTablesOutput{RouteTables: emptyTables}, nil)
		// No DescribeInstances / CreateRoute calls are expected: gomock fails the test
		// if any of the malicious routes reach the AWS create path.
		result, err := customRoutes.Update(ctx, maliciousRoutes, func() {})
		Expect(err).To(BeNil())
		Expect(result).NotTo(BeNil())
		for _, mr := range maliciousRoutes {
			Expect(result.SuccessfulRoutes[mr.PodCIDR]).To(BeFalse())
		}
	})

	It("should still create legitimate in-network routes when mixed with out-of-network ones", func() {
		mixedRoutes := []updater.NodeRoute{
			{InstanceID: "i-legit", PodCIDR: "10.243.3.0/24"}, // in pod network -> created
			{InstanceID: "i-attacker", PodCIDR: "0.0.0.0/1"},  // out of network -> skipped
		}
		emptyTables := []ec2types.RouteTable{
			{
				RouteTableId: rt1,
				Tags:         []ec2types.Tag{clusterTag},
				Routes:       []ec2types.Route{route1},
			},
		}
		ec2RoutesMock.EXPECT().DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{}).Return(&ec2.DescribeRouteTablesOutput{RouteTables: emptyTables}, nil)
		ec2RoutesMock.EXPECT().DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: []string{"i-legit"},
		}).Return(&ec2.DescribeInstancesOutput{
			Reservations: []ec2types.Reservation{
				{
					Instances: []ec2types.Instance{
						{
							InstanceId: aws.String("i-legit"),
							NetworkInterfaces: []ec2types.InstanceNetworkInterface{
								{NetworkInterfaceId: aws.String("eni-legit")},
							},
						},
					},
				},
			},
		}, nil)
		ec2RoutesMock.EXPECT().CreateRoute(ctx, &ec2.CreateRouteInput{
			DestinationCidrBlock: aws.String("10.243.3.0/24"),
			InstanceId:           aws.String("i-legit"),
			RouteTableId:         rt1,
		})
		result, err := customRoutes.Update(ctx, mixedRoutes, func() {})
		Expect(err).To(BeNil())
		Expect(result).NotTo(BeNil())
		Expect(result.SuccessfulRoutes["10.243.3.0/24"]).To(BeTrue())
		Expect(result.SuccessfulRoutes["0.0.0.0/1"]).To(BeFalse())
	})

})
