// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package updater_test

import (
	"context"

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
		ec2RoutesMock.EXPECT().CreateRoute(ctx, &ec2.CreateRouteInput{
			DestinationCidrBlock: aws.String(nodeRoutes[1].PodCIDR),
			InstanceId:           aws.String(nodeRoutes[1].InstanceID),
			RouteTableId:         rt1,
		})
		ec2RoutesMock.EXPECT().CreateRoute(ctx, &ec2.CreateRouteInput{
			DestinationCidrBlock: aws.String(nodeRoutes[0].PodCIDR),
			InstanceId:           aws.String(nodeRoutes[0].InstanceID),
			RouteTableId:         rt2,
		})
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

})
