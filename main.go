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
	promqlParser "github.com/prometheus/prometheus/promql/parser"
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

	var parsedExpr promqlParser.Expr
	var err error
	if expr != "" {
		parsedExpr, err = promqlParser.ParseExpr(expr)
		if err != nil {
			log.Fatalf("Could not parse expr: %s, err=%s", expr, err.Error())
		}
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
	err = ingestMetrics(storage, metricsFamilies)
	if err != nil {
		log.Fatalf("Could not ingest prometheus metrics: err=%s", err.Error())
	}
	if parsedExpr == nil {
		fmt.Println("TODO execute expr")
	}

}

func ingestMetrics(st storage.Storage, metricsFamilies []*io_prometheus_client.MetricFamily) error {
	appender := st.Appender(context.Background())

	defer appender.Commit()
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
	return nil
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
