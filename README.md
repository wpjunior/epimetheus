# epimetheus
Tool to scrape prometheus metrics and execute some queries for debug/troubleshooting/CI purposes

# usage

```
epimetheus -url http://myservice/metrics -expr 'sum(rate(http_requests_total[2m]))'
```

at every 5 seconds epimetheus will evaluate the query against my service endpoint.
