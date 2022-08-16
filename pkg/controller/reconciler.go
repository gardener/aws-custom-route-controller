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

	"github.com/gardener/aws-custom-route-controller/pkg/updater"
	"github.com/go-logr/logr"
	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// NodeReconciler watches Nodes for pod CIDRs to update route table(s)
type NodeReconciler struct {
	client.Client

	log                logr.Logger
	initialiseStarted  atomic.Bool
	initialiseFinished atomic.Bool
	nodeRoutes         *updater.NamedNodeRoutes
	lastTick           atomic.Time
	tickPeriod         time.Duration
}

// NewNodeReconciler creates a NodeReconciler instance
func NewNodeReconciler() *NodeReconciler {
	return &NodeReconciler{
		nodeRoutes: updater.NewNamedNodeRoutes(),
	}
}

// StartUpdater starts background go routine to check for changed routes calculated by watching nodes
func (r *NodeReconciler) StartUpdater(ctx context.Context, updateFunc updater.NodeRoutesUpdater,
	tickPeriod, syncPeriod, maxDelayOnFailure time.Duration) {
	r.tickPeriod = tickPeriod
	ticker := time.NewTicker(tickPeriod)
	log := r.log.WithName("ticker")

	go func() {
		var lastUpdate time.Time
		var lastFailure time.Time
		var delay time.Duration
		for {
			select {
			case <-ticker.C:
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
					if err := updateFunc(routes); err != nil {
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
					lastUpdate = time.Now()
				}
				r.lastTick.Store(time.Now())
			}
		}
	}()
}

// Reconcile extracts pod cidrs from nodes
func (r *NodeReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	if r.initialiseStarted.CompareAndSwap(false, true) {
		r.initialise(ctx)
	}

	node := &corev1.Node{}
	err := r.Get(ctx, req.NamespacedName, node)
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
	if !r.initialiseFinished.Load() {
		return fmt.Errorf("not initialised")
	}
	return nil
}

func (r *NodeReconciler) HealthzChecker(_ *http.Request) error {
	if r.lastTick.Load().Add(3 * r.tickPeriod).Before(time.Now()) {
		return fmt.Errorf("missing tick")
	}
	return nil
}

func (r *NodeReconciler) initialise(ctx context.Context) {
	r.log.Info("initialise started")
	nodeList := &corev1.NodeList{}
	if err := r.Client.List(ctx, nodeList); err != nil {
		r.log.Error(err, "listing nodes failed")
		panic(err) // to avoid cleaning routing table
	}
	for _, node := range nodeList.Items {
		r.addNodeRoute(&node)
	}
	r.initialiseFinished.Store(true)
	r.log.Info("initialise finished")
}

func (r *NodeReconciler) InjectClient(c client.Client) error {
	r.Client = c
	return nil
}

func (r *NodeReconciler) InjectLogger(l logr.Logger) error {
	r.log = l.WithName("controller").WithName("node")
	return nil
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
