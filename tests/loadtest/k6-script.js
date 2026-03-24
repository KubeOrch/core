import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
  stages: [
    { duration: "10s", target: 10 }, // ramp up to 10 users
    { duration: "30s", target: 50 }, // stay at 50 users
    { duration: "10s", target: 0 }, // ramp down
  ],
  thresholds: {
    http_req_duration: ["p(95)<500"], // 95% of requests under 500ms
    http_req_failed: ["rate<0.01"], // less than 1% failure rate
  },
};

const BASE_URL = __ENV.BASE_URL || "http://localhost:3000";

export default function () {
  // Test hello endpoint
  const helloRes = http.get(`${BASE_URL}/v1/`);
  check(helloRes, {
    "hello status 200": (r) => r.status === 200,
    "hello has message": (r) => r.json().message !== undefined,
  });

  // Test metrics endpoint
  const metricsRes = http.get(`${BASE_URL}/metrics`);
  check(metricsRes, {
    "metrics status 200": (r) => r.status === 200,
    "metrics has content": (r) => r.body.includes("http_requests_total"),
  });

  sleep(0.1);
}
