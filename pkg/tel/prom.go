package tel

import (
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func ServeMetrics() {
	address := os.Getenv("METRICS_ADDRESS")
	if address == "-" {
		return
	}
	if address == "" {
		address = ":9001"
	}
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/stats/prometheus", promhttp.Handler())

	go http.ListenAndServe(address, nil)
}

