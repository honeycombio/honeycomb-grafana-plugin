package main

import (
	"os"

	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/plugin"
)

func main() {
	if err := datasource.Manage(
		"honeycombio-honeycomb-datasource",
		plugin.NewDatasource,
		datasource.ManageOpts{},
	); err != nil {
		log.DefaultLogger.Error("Failed to start Honeycomb datasource plugin", "error", err)
		os.Exit(1)
	}
}
