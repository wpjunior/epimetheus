package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/teststorage"
)

func main() {
	var url string
	var expr string
	flag.StringVar(&url, "url", "", "URL to scrape")
	flag.StringVar(&expr, "expr", "", "Expr to evaluate")
	flag.Parse()

	if url == "" {
		log.Fatal("You must provide an URL")
	}

	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("Could not get URL: %s, err=%s", url, err.Error())
	}
	if resp.StatusCode >= http.StatusBadRequest {
		log.Fatalf("Could not get URL: %s, statusCode=%d", url, resp.StatusCode)
	}
	defer resp.Body.Close()

	metricsFamilies, err := decodeMetrics(resp.Body)
	if err != nil {
		log.Fatalf("Could not decode prometheus metrics: err=%s", err.Error())
	}
	storage := teststorage.New(&log.Logger{})
	defer storage.Close()
	err = ingestMetrics(storage, metricsFamilies)
	if err != nil {
		log.Fatalf("Could not ingest prometheus metrics: err=%s", err.Error())
	}

	engine := promql.NewEngine(promql.EngineOpts{
		//Reg: storage,
		LookbackDelta: time.Minute * 5,
		Timeout:       time.Second * 10,
	})
	query, err := engine.NewInstantQuery(storage, expr, time.Now())
	if err != nil {
		log.Fatalf("Could not create query: err=%s", err.Error())
	}
	result := query.Exec(context.Background())
	if result.Err != nil {
		log.Fatalf("Could not execute query: err=%s", result.Err.Error())
	}

	fmt.Println("type of result", result.Value.Type())
	vector, err := result.Vector()
	if err != nil {
		log.Fatalf("Could not create vector: err=%s", err.Error())
	}

	fmt.Println("lenvector", len(vector))

}

func ingestMetrics(st storage.Storage, metricsFamilies []*io_prometheus_client.MetricFamily) error {
	appender := st.Appender(context.Background())

	now := time.Now()
	for _, mf := range metricsFamilies {
		for _, m := range mf.Metric {
			metricLabels := labels.Labels{
				{
					Name:  "__name__",
					Value: mf.GetName(),
				},
			}
			for _, label := range m.Label {
				metricLabels = append(metricLabels, labels.Label{
					Name:  label.GetName(),
					Value: label.GetValue(),
				})
			}
			var value float64
			if m.Counter != nil {
				value = m.Counter.GetValue()

			} else if m.Gauge != nil {
				value = m.Gauge.GetValue()
			} else if m.Histogram != nil {
				log.Println("TODO ingest histogram")
			} else if m.Summary != nil {
				value = m.Summary.GetSampleSum()
			} else if m.Untyped != nil {
				value = m.Untyped.GetValue()
				fmt.Println(m)
			}

			_, err := appender.Add(metricLabels, now.Unix(), value)
			if err != nil {
				return err
			}
		}
	}

	return appender.Commit()
}

func decodeMetrics(r io.Reader) ([]*io_prometheus_client.MetricFamily, error) {
	decoder := expfmt.NewDecoder(r, expfmt.FmtText)

	metricFamilies := []*io_prometheus_client.MetricFamily{}
	for {
		mf := &io_prometheus_client.MetricFamily{}
		err := decoder.Decode(mf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		metricFamilies = append(metricFamilies, mf)
	}

	return metricFamilies, nil
}
