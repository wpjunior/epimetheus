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
	"github.com/prometheus/prometheus/pkg/timestamp"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/teststorage"
)

func main() {
	var url string
	var expr string
	var interval int
	flag.StringVar(&url, "url", "", "URL to scrape")
	flag.StringVar(&expr, "expr", "", "Expr to evaluate")
	flag.IntVar(&interval, "interval", 5, "scrape metrics every interval")
	flag.Parse()

	if url == "" {
		log.Fatal("You must provide an URL")
	}

	storage := teststorage.New(&log.Logger{})
	defer storage.Close()

	for {
		reader, err := readerForURL(url)
		if err != nil {
			log.Fatal(err.Error())
		}
		metricsFamilies, err := decodeMetrics(reader)
		if err != nil {
			log.Fatalf("Could not decode prometheus metrics: err=%s", err.Error())
		}
		reader.Close()

		err = ingestMetrics(storage, metricsFamilies)
		if err != nil {
			log.Fatalf("Could not ingest prometheus metrics: err=%s", err.Error())
		}

		engine := promql.NewEngine(promql.EngineOpts{
			MaxSamples:    10000,
			LookbackDelta: time.Minute * 5,
			Timeout:       time.Second * 10,
		})
		query, err := engine.NewInstantQuery(storage, expr, timestamp.Time(time.Now().Unix()))
		if err != nil {
			log.Fatalf("Could not create query: err=%s", err.Error())
		}
		result := query.Exec(context.Background())
		if result.Err != nil {
			log.Fatalf("Could not execute query: err=%s", result.Err.Error())
		}

		switch v := result.Value.(type) {
		case promql.Vector:
			for _, item := range v {
				fmt.Println(item.Metric, item.Point.V)
			}
		}

		time.Sleep(time.Second * time.Duration(interval))
	}
}

func readerForURL(url string) (io.ReadCloser, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("Could not get URL: %s, err=%s", url, err.Error())
	}
	if resp.StatusCode >= http.StatusBadRequest {
		resp.Body.Close()
		return nil, fmt.Errorf("Could not get URL: %s, statusCode=%d", url, resp.StatusCode)
	}

	return resp.Body, nil
}

func ingestMetrics(st storage.Storage, metricsFamilies []*io_prometheus_client.MetricFamily) error {
	appender := st.Appender(context.Background())

	now := time.Now().Round(time.Second)
	for _, mf := range metricsFamilies {
		for _, m := range mf.Metric {
			metricLabels := labels.FromStrings(labels.MetricName, mf.GetName())
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
