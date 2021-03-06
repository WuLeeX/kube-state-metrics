/*
Copyright 2017 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package collectors

import (
	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

var (
	descComponentStatusStatusHealthy = prometheus.NewDesc(
		"kube_componentstatus_status_healthy",
		"kube component status healthy status.",
		[]string{"name", "status"}, nil,
	)
)

type ComponentStatusLister func() (v1.ComponentStatusList, error)

func (csl ComponentStatusLister) List() (v1.ComponentStatusList, error) {
	return csl()
}

func RegisterComponentStatusCollector(registry prometheus.Registerer, kubeClient kubernetes.Interface, namespace string) {
	client := kubeClient.CoreV1().RESTClient()
	glog.Infof("collect componentstatuses with %s", client.APIVersion())
	slw := cache.NewListWatchFromClient(client, "componentstatuses", v1.NamespaceAll, fields.Everything())
	sinf := cache.NewSharedInformer(slw, &v1.ComponentStatus{}, resyncPeriod)

	componentStatusLister := ComponentStatusLister(func() (componentStatuses v1.ComponentStatusList, err error) {
		for _, m := range sinf.GetStore().List() {
			componentStatuses.Items = append(componentStatuses.Items, *m.(*v1.ComponentStatus))
		}
		return componentStatuses, nil
	})

	registry.MustRegister(&componentStatusCollector{store: componentStatusLister})
	go sinf.Run(context.Background().Done())
}

type componentStatusStore interface {
	List() (componentStatuses v1.ComponentStatusList, err error)
}

// componentStatusCollector collects metrics about all components in the cluster.
type componentStatusCollector struct {
	store componentStatusStore
}

// Describe implements the prometheus.Collector interface.
func (csc *componentStatusCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descComponentStatusStatusHealthy
}

// Collect implements the prometheus.Collector interface.
func (csc *componentStatusCollector) Collect(ch chan<- prometheus.Metric) {
	csl, err := csc.store.List()
	if err != nil {
		ScrapeErrorTotalMetric.With(prometheus.Labels{"resource": "componentstatus"}).Inc()
		glog.Errorf("listing component status failed: %s", err)
		return
	}

	ResourcesPerScrapeMetric.With(prometheus.Labels{"resource": "componentstatus"}).Observe(float64(len(csl.Items)))
	for _, s := range csl.Items {
		csc.collectComponentStatus(ch, s)
	}
	glog.Infof("collected %d componentstatuses", len(csl.Items))
}

func (csc *componentStatusCollector) collectComponentStatus(ch chan<- prometheus.Metric, s v1.ComponentStatus) {
	addConstMetric := func(desc *prometheus.Desc, t prometheus.ValueType, v float64, lv ...string) {
		lv = append([]string{s.Name}, lv...)
		ch <- prometheus.MustNewConstMetric(desc, t, v, lv...)
	}
	addGauge := func(desc *prometheus.Desc, v float64, lv ...string) {
		addConstMetric(desc, prometheus.GaugeValue, v, lv...)
	}
	for _, p := range s.Conditions {
		if p.Type == v1.ComponentHealthy {
			addGauge(descComponentStatusStatusHealthy, boolFloat64(p.Status == v1.ConditionTrue), string(v1.ConditionTrue))
			addGauge(descComponentStatusStatusHealthy, boolFloat64(p.Status == v1.ConditionFalse), string(v1.ConditionFalse))
			addGauge(descComponentStatusStatusHealthy, boolFloat64(p.Status == v1.ConditionUnknown), string(v1.ConditionUnknown))
			break
		}
	}
}
