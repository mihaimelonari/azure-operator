package terminateunhealthynode

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	gaugeValue float64 = 1
)

var (
	nodeAutoRepairTermination = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azure_operator_unhealthy_node_termination",
			Help: "Gauge representing node termination due to node auto repair feature.",
		},
		[]string{"cluster_id", "terminated_node", "terminated_instance_id"},
	)
)

func init() {
	prometheus.MustRegister(nodeAutoRepairTermination)

}

// reportNodeTermination is a utility function for updating metrics related to
// node auto repair node termination.
func reportNodeTermination(clusterID string, nodeName string, instanceID string) {
	nodeAutoRepairTermination.WithLabelValues(
		clusterID, nodeName, instanceID,
	).Set(gaugeValue)
}
