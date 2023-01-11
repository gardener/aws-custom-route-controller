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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// NodeReconciler watches Nodes for pod CIDRs to update route table(s)
type NodeReconciler struct {
	client.Client

	log                logr.Logger
	initialiseStarted  atomic.Bool
	initialiseFinished atomic.Bool
	updaterStarted     atomic.Bool
	elected            <-chan struct{}
	nodeRoutes         *updater.NamedNodeRoutes
	lastTick           atomic.Time
	tickPeriod         time.Duration

	recorder    record.EventRecorder
	lastEventOk bool
}

// NewNodeReconciler creates a NodeReconciler instance
func NewNodeReconciler(elected <-chan struct{}, recorder record.EventRecorder) *NodeReconciler {
	return &NodeReconciler{
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
					err := updateFunc(routes)
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
					lastUpdate = time.Now()
				}
				r.lastTick.Store(time.Now())
			}
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
		r.recorder.Event(ref, corev1.EventTypeNormal, "RoutesUpToDate", "routes for all route tables are up-to-date")
	} else {
		msg := err.Error()
		if len(msg) > 300 {
			msg = msg[:300] + "..."
		}
		r.recorder.Event(ref, corev1.EventTypeWarning, "RoutesUpdateFailed", msg)
	}
	r.lastEventOk = isOk
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
	if !r.updaterStarted.Load() {
		return fmt.Errorf("updater not started")
	}
	return nil
}

func (r *NodeReconciler) HealthzChecker(_ *http.Request) error {
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
