/*
 * SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
 *
 * SPDX-License-Identifier: Apache-2.0
 */

package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/events"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/gardener/aws-custom-route-controller/pkg/updater"
	"github.com/gardener/aws-custom-route-controller/pkg/util"
)

var (
	// updateNetworkConditionBackoff is the backoff for updating node condition
	updateNetworkConditionBackoff = wait.Backoff{
		Steps:    10,
		Duration: 200 * time.Millisecond,
		Factor:   1.5,
		Jitter:   0.1,
	}
)

// NodeReconciler watches Nodes for pod CIDRs to update route table(s)
type NodeReconciler struct {
	client client.Client

	log                logr.Logger
	initialiseStarted  atomic.Bool
	initialiseFinished atomic.Bool
	updaterStarted     atomic.Bool
	elected            <-chan struct{}
	nodeRoutes         *updater.NamedNodeRoutes
	lastTick           atomic.Time
	tickPeriod         time.Duration

	recorder    events.EventRecorder
	lastEventOk bool
}

// NewNodeReconciler creates a NodeReconciler instance
func NewNodeReconciler(
	client client.Client,
	log logr.Logger,
	elected <-chan struct{},
	recorder events.EventRecorder,
) *NodeReconciler {
	return &NodeReconciler{
		client:     client,
		log:        log.WithName("controller").WithName("node"),
		elected:    elected,
		nodeRoutes: updater.NewNamedNodeRoutes(),
		recorder:   recorder,
	}
}

// StartUpdater starts background go routine to check for changed routes calculated by watching nodes
func (r *NodeReconciler) StartUpdater(ctx context.Context, updateFunc updater.NodeRoutesUpdater,
	tickPeriod, syncPeriod, maxDelayOnFailure time.Duration) {
	r.tickPeriod = tickPeriod
	ticker := time.NewTicker(tickPeriod)
	log := r.log.WithName("ticker")

	go func() {
		var (
			lastUpdate  time.Time
			lastFailure time.Time
			delay       time.Duration
		)

		r.updaterStarted.Store(true)

		for {
			<-ticker.C
			if ctx.Err() != nil {
				log.Info("updater loop cancelled")
				return
			}
			if !r.initialiseFinished.Load() {
				continue
			}
			if lastUpdate.Add(syncPeriod).Before(time.Now()) {
				log.Info("sync")
				r.nodeRoutes.SetChanged()
			}
			if delay > 0 && lastFailure.Add(delay).Before(time.Now()) {
				log.Info("retry")
				r.nodeRoutes.SetChanged()
			}
			if routes := r.nodeRoutes.GetRoutesIfChanged(); routes != nil {
				result, err := updateFunc(ctx, routes, func() { r.lastTick.Store(time.Now()) })
				if err != nil {
					log.Error(err, "updating routes failed")
					lastFailure = time.Now()
					if delay == 0 {
						delay = tickPeriod
					} else {
						delay = 4 * delay / 3
						if delay > maxDelayOnFailure {
							delay = maxDelayOnFailure
						}
					}
				} else {
					delay = 0
				}
				r.reportEventIfNeeded(err)

				// Update node conditions based on route creation results
				if result != nil {
					r.updateNodeConditions(ctx, routes, result)
				}

				lastUpdate = time.Now()
			}
			r.lastTick.Store(time.Now())
		}
	}()
}

func (r *NodeReconciler) reportEventIfNeeded(err error) {
	isOk := err == nil
	if isOk && r.lastEventOk {
		// only single good event is sent (initial or after recovery)
		return
	}

	// An object is needed this event is about.
	// As the aws-custom-route-controller has not many objects in the shoot cluster, just use its ServiceAccount.
	ref := &corev1.ObjectReference{
		Kind:       "ServiceAccount",
		APIVersion: "v1",
		Namespace:  metav1.NamespaceSystem,
		Name:       "aws-custom-route-controller",
	}
	if isOk {
		r.recorder.Eventf(ref, nil, corev1.EventTypeNormal, "RoutesUpToDate", "Reconciling", "routes for all route tables are up-to-date")
	} else {
		msg := err.Error()
		if len(msg) > 300 {
			msg = msg[:300] + "..."
		}
		r.recorder.Eventf(ref, nil, corev1.EventTypeWarning, "RoutesUpdateFailed", "Reconciling", msg)
	}
	r.lastEventOk = isOk
}

// Reconcile extracts pod cidrs from nodes
func (r *NodeReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	if r.initialiseStarted.CompareAndSwap(false, true) {
		r.initialise(ctx)
	}

	node := &corev1.Node{}
	err := r.client.Get(ctx, req.NamespacedName, node)
	if err != nil {
		if errors.IsNotFound(err) {
			r.removeNodeRoute(req.Name)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	r.addNodeRoute(node)

	return reconcile.Result{}, nil
}

func (r *NodeReconciler) ReadyChecker(_ *http.Request) error {
	if !r.updaterStarted.Load() {
		return fmt.Errorf("updater not started")
	}
	return nil
}

func (r *NodeReconciler) HealthzChecker(_ *http.Request) error {
	err := r.healthzChecker()
	if err != nil {
		r.log.Error(err, "healthz check failed")
	}
	return err
}

func (r *NodeReconciler) healthzChecker() error {
	if !r.initialiseFinished.Load() {
		if !r.initialiseStarted.Load() {
			select {
			case <-r.elected:
				return fmt.Errorf("initialise not started")
			default:
				// waiting for leader election
				return nil
			}
		}
		return fmt.Errorf("initialise not finished")
	}
	if r.lastTick.Load().Add(5 * r.tickPeriod).Before(time.Now()) {
		return fmt.Errorf("missing tick")
	}
	return nil
}

func (r *NodeReconciler) initialise(ctx context.Context) {
	r.log.Info("initialise started")
	nodeList := &corev1.NodeList{}
	if err := r.client.List(ctx, nodeList); err != nil {
		r.log.Error(err, "listing nodes failed")
		panic(err) // to avoid cleaning routing table
	}
	for _, node := range nodeList.Items {
		r.addNodeRoute(&node)
	}
	r.initialiseFinished.Store(true)
	r.log.Info("initialise finished")
}

func (r *NodeReconciler) addNodeRoute(node *corev1.Node) {
	if route, changed := r.nodeRoutes.AddNodeRoute(node); changed {
		r.log.Info("added node route", "node", node.Name, "podCIDR", route.PodCIDR, "instanceID", route.InstanceID)
	}
}

func (r *NodeReconciler) removeNodeRoute(nodeName string) {
	if route := r.nodeRoutes.RemoveNodeRoute(nodeName); route != nil {
		r.log.Info("removed node route", "node", nodeName, "podCIDR", route.PodCIDR, "instanceID", route.InstanceID)
	}
}

// updateNetworkingCondition updates the NetworkUnavailable condition for a node based on route creation status
func (r *NodeReconciler) updateNetworkingCondition(ctx context.Context, node *corev1.Node, routesCreated bool) error {
	_, condition := util.GetNodeCondition(&node.Status, corev1.NodeNetworkUnavailable)

	if routesCreated && condition != nil && condition.Status == corev1.ConditionFalse && condition.Reason == "RouteCreated" {
		r.log.Info("set node %v with NodeNetworkUnavailable=false was canceled because it is already set", node.Name)
		return nil
	}

	if !routesCreated && condition != nil && condition.Status == corev1.ConditionTrue && condition.Reason == "NoRouteCreated" {
		r.log.Info("set node %v with NodeNetworkUnavailable=true was canceled because it is already set", node.Name)
		return nil
	}

	klog.Infof("Patching node status %v with %v previous condition was:%+v", node.Name, routesCreated, condition)

	// either condition is not there, or has a value != to what we need
	// start setting it
	err := wait.ExponentialBackoff(updateNetworkConditionBackoff, func() (bool, error) {
		var err error
		// Patch could also fail, even though the chance is very slim. So we still do
		// patch in the retry loop.
		currentTime := metav1.Now()
		if routesCreated {
			err = util.SetNodeCondition(ctx, r.client, types.NodeName(node.Name), corev1.NodeCondition{
				Type:               corev1.NodeNetworkUnavailable,
				Status:             corev1.ConditionFalse,
				Reason:             "RouteCreated",
				Message:            "RouteController created a route",
				LastTransitionTime: currentTime,
			})
		} else {
			err = util.SetNodeCondition(ctx, r.client, types.NodeName(node.Name), corev1.NodeCondition{
				Type:               corev1.NodeNetworkUnavailable,
				Status:             corev1.ConditionTrue,
				Reason:             "NoRouteCreated",
				Message:            "RouteController failed to create a route",
				LastTransitionTime: currentTime,
			})
		}
		if err != nil {
			klog.V(4).Infof("Error updating node %s, retrying: %v", types.NodeName(node.Name), err)
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		klog.Errorf("Error updating node %s: %v", node.Name, err)
	}

	return err
}

// updateNodeConditions updates the NetworkUnavailable condition for all nodes based on route update results
func (r *NodeReconciler) updateNodeConditions(ctx context.Context, routes []updater.NodeRoute, result *updater.RouteUpdateResult) {
	// Create a map of podCIDR to node for quick lookup
	nodeList := &corev1.NodeList{}
	if err := r.client.List(ctx, nodeList); err != nil {
		r.log.Error(err, "failed to list nodes for condition update")
		return
	}

	// Create a map for quick node lookup by podCIDR
	podCIDRToNode := make(map[string]*corev1.Node)
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if node.Spec.PodCIDR != "" {
			podCIDRToNode[node.Spec.PodCIDR] = node
		}
	}

	// Update conditions for each route
	for _, route := range routes {
		if node, ok := podCIDRToNode[route.PodCIDR]; ok {
			routeSuccess := result.SuccessfulRoutes[route.PodCIDR]
			if err := r.updateNetworkingCondition(ctx, node, routeSuccess); err != nil {
				r.log.Error(err, "failed to update node condition", "node", node.Name, "podCIDR", route.PodCIDR)
			}
		}
	}
}
