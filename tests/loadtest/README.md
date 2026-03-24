# Load Testing

## Go Benchmarks

Run the built-in Go benchmarks:

```bash
# Run all benchmarks
go test ./tests/... -bench=. -benchmem -count=3

# Run specific benchmark
go test ./tests/... -bench=BenchmarkHelloEndpoint -benchmem

# Run with CPU profiling
go test ./tests/... -bench=BenchmarkFullMiddlewareStack -cpuprofile=cpu.prof
go tool pprof cpu.prof
```

## Using k6 (External Load Testing)

Install [k6](https://k6.io/docs/get-started/installation/):

```bash
# macOS
brew install k6

# Linux
sudo gpg -k && sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D68
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update && sudo apt-get install k6
```

Run the load test (ensure the server is running on localhost:3000):

```bash
k6 run tests/loadtest/k6-script.js
```

## Using vegeta (Alternative)

```bash
go install github.com/tsenart/vegeta@latest

# 100 requests/sec for 30 seconds against the hello endpoint
echo "GET http://localhost:3000/v1/" | vegeta attack -rate=100 -duration=30s | vegeta report

# Against the metrics endpoint
echo "GET http://localhost:3000/metrics" | vegeta attack -rate=50 -duration=10s | vegeta report
```
