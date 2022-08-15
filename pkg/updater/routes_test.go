// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package updater_test

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/gardener/aws-custom-route-controller/pkg/updater"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var _ = Describe("CustomRoutes", func() {
	var (
		ctrl          *gomock.Controller
		customRoutes  *updater.CustomRoutes
		ec2RoutesMock *updater.MockEC2Routes
		clusterName   = "shoot--foo--bar"
		clusterTag    = &ec2.Tag{
			Key:   aws.String(updater.ClusterTagKey(clusterName)),
			Value: aws.String("1"),
		}
		route1 = &ec2.Route{
			DestinationCidrBlock: aws.String("0.0.0.0/0"),
			NatGatewayId:         aws.String("nat123"),
			Origin:               aws.String(ec2.RouteOriginCreateRouteTable),
		}
		route2 = &ec2.Route{
			DestinationCidrBlock: aws.String("10.243.222.0/24"),
			InstanceId:           aws.String("i-another"),
			Origin:               aws.String(ec2.RouteOriginCreateRoute),
		}
		routeNode1 = &ec2.Route{
			DestinationCidrBlock: aws.String("10.243.3.0/24"),
			InstanceId:           aws.String("i-node1"),
			Origin:               aws.String(ec2.RouteOriginCreateRoute),
		}
		routeNode2 = &ec2.Route{
			DestinationCidrBlock: aws.String("10.243.9.0/24"),
			InstanceId:           aws.String("i-node2"),
			Origin:               aws.String(ec2.RouteOriginCreateRoute),
		}
		routeNode3 = &ec2.Route{
			DestinationCidrBlock: aws.String("10.243.13.0/24"),
			InstanceId:           aws.String("i-node3"),
			Origin:               aws.String(ec2.RouteOriginCreateRoute),
		}
		rt1    = aws.String("rt1")
		rt2    = aws.String("rt2")
		rt3    = aws.String("rt3")
		tables = []*ec2.RouteTable{
			{
				RouteTableId: rt1,
				Tags:         []*ec2.Tag{clusterTag},
				Routes: []*ec2.Route{
					route1,
					route2,
					routeNode1,
					routeNode2,
				},
			},
			{
				RouteTableId: rt2,
				Tags:         []*ec2.Tag{clusterTag},
				Routes: []*ec2.Route{
					route1,
					route2,
				},
			},
			{
				RouteTableId: rt3,
				Routes: []*ec2.Route{
					routeNode1,
				},
			},
		}
		tables2 = []*ec2.RouteTable{
			{
				RouteTableId: rt1,
				Tags:         []*ec2.Tag{clusterTag},
				Routes: []*ec2.Route{
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
		ec2RoutesMock.EXPECT().DescribeRouteTables(&ec2.DescribeRouteTablesInput{}).Return([]*ec2.RouteTable{}, nil)
		err := customRoutes.Update(nil)
		Expect(err).NotTo(BeNil())
	})

	It("should update route tables", func() {
		ec2RoutesMock.EXPECT().DescribeRouteTables(&ec2.DescribeRouteTablesInput{}).Return(tables, nil)
		ec2RoutesMock.EXPECT().DeleteRoute(&ec2.DeleteRouteInput{
			DestinationCidrBlock: routeNode2.DestinationCidrBlock,
			RouteTableId:         rt1,
		})
		ec2RoutesMock.EXPECT().CreateRoute(&ec2.CreateRouteInput{
			DestinationCidrBlock: aws.String(nodeRoutes[1].PodCIDR),
			InstanceId:           aws.String(nodeRoutes[1].InstanceID),
			RouteTableId:         rt1,
		})
		ec2RoutesMock.EXPECT().CreateRoute(&ec2.CreateRouteInput{
			DestinationCidrBlock: aws.String(nodeRoutes[0].PodCIDR),
			InstanceId:           aws.String(nodeRoutes[0].InstanceID),
			RouteTableId:         rt2,
		})
		ec2RoutesMock.EXPECT().CreateRoute(&ec2.CreateRouteInput{
			DestinationCidrBlock: aws.String(nodeRoutes[1].PodCIDR),
			InstanceId:           aws.String(nodeRoutes[1].InstanceID),
			RouteTableId:         rt2,
		})
		err := customRoutes.Update(nodeRoutes)
		Expect(err).To(BeNil())
	})

	It("should update nothing if unchanged", func() {
		ec2RoutesMock.EXPECT().DescribeRouteTables(&ec2.DescribeRouteTablesInput{}).Return(tables2, nil)
		err := customRoutes.Update(nodeRoutes)
		Expect(err).To(BeNil())
	})

})
