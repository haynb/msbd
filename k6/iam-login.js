import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: Number(__ENV.K6_VUS || 10),
  duration: __ENV.K6_DURATION || '1m',
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<300'],
  },
};

const BASE_URL = __ENV.K6_BASE_URL || 'http://localhost:8080';
const EMAIL = __ENV.K6_EMAIL || 'admin@cloud-access.local';
const PASSWORD = __ENV.K6_PASSWORD || 'ChangeMe!2024';

export default function () {
  const payload = JSON.stringify({
    email: EMAIL,
    password: PASSWORD,
    client_kind: 'load-test',
  });

  const res = http.post(`${BASE_URL}/auth/login`, payload, {
    headers: { 'Content-Type': 'application/json' },
  });

  check(res, {
    'status is 200': (r) => r.status === 200,
    'contains access token': (r) => r.json('access_token') !== '',
  });

  sleep(Number(__ENV.K6_SLEEP || 1));
}
