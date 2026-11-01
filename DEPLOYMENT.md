# LivePoll deployment setup

This repository keeps the Vercel frontend and Render API in separate services. No secrets belong in Git or in this document. Rotate the credentials previously pasted in chat before using them in provider settings.

## Backend: Render, Atlas, and Redis Cloud

The root `render.yaml` defines a Docker web service on Render's **Starter** plan, with `/api/health` as its health check and a 15-second shutdown window. Starter or a higher paid plan stays available; Render's Free web services can spin down after 15 minutes without inbound activity.

Render builds the backend from `backend/Dockerfile` using the repository root as Docker context. The image build command is:

```sh
CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/livepoll ./cmd/server
```

The container start command is `/app/livepoll`. Render supplies `PORT` at runtime. The server starts the REST API, WebSocket handlers, vote stream worker, reconciliation ticker, and expiry ticker in one process.

Set these environment variable names in the Render service settings. Enter secret values directly in Render; the Blueprint leaves them unsynchronized:

- `MONGO_URI`
- `REDIS_URL`
- `JWT_SECRET`
- `CLIENT_URL`
- `COOKIE_SECURE`
- `TRUST_PROXY_HEADERS`

Set `COOKIE_SECURE` to `true` in production. Use the Atlas connection URI and Redis Cloud TCP URI from their provider dashboards; use the Redis TLS form (`rediss://`) when TLS is enabled. `JWT_SECRET` must be a private random value of at least 32 characters. Set `CLIENT_URL` to the exact Vercel production origin, with no path or trailing slash. Render supplies `PORT` automatically.

Set `TRUST_PROXY_HEADERS` to `true` on Render. Render documents `X-Forwarded-For` as the client IP header. The backend uses the rightmost valid IP in that chain, since proxies append to it, and falls back to the socket address when proxy trust is disabled or the header is invalid. Keep it `false` for local development.

The optional backend settings `WS_PING_INTERVAL`, `WS_PONG_WAIT`, `WS_WRITE_WAIT`, `WS_MAX_MESSAGE_BYTES`, and `WS_MAX_CONNECTIONS_PER_IP` are documented in `backend/.env.example`; Render defaults are used unless you add overrides.

The Render URL will be `https://<render-service>.onrender.com`. Atlas must allow network connections from Render. Use an Atlas network rule appropriate for your Render egress configuration; avoid opening database access broadly when a narrower allowlist is available.

## Frontend: Vercel

Create or configure the Vercel project with:

- Root Directory: `frontend`
- Framework Preset: Vite
- Install Command: `npm ci`
- Build Command: `npm run build`
- Output Directory: `dist`

Set `VITE_API_URL` in Vercel's **Production** environment before the first deployment. Its value is the Render API origin, such as `https://<render-service>.onrender.com`, without a path. Vite embeds it at build time; changing it requires a new deployment. This is the only required Vercel environment variable.

`frontend/vercel.json` rewrites client-side routes to `/index.html`, so poll links and browser refreshes load through the React app. Both WebSocket clients convert the configured API origin to `ws://` or `wss://`; with an empty API URL they use the current host, which works with a configured Vite development proxy.

Set the Render `CLIENT_URL` to the exact production Vercel origin. To support Vercel preview deployments too, provide a comma-separated list of exact origins in `CLIENT_URL`, for example `https://your-production-domain.vercel.app,https://your-preview-domain.vercel.app`. CORS and WebSocket origin checks allow only listed origins; do not use wildcards. Voter requests carry a stable UUID from frontend local storage, so duplicate-vote protection does not depend on cross-site cookies. The voter cookie remains an HttpOnly fallback; in production it is `Secure` and `SameSite=None`.

## Run order

1. Review and commit the deployment changes in this repository. Keep `.env` files local and untracked.
2. Rotate the exposed Atlas, Redis Cloud, and JWT credentials, then enter the new values directly in provider settings.
3. Create the Render web service from `render.yaml` (or enter the same Docker settings manually). Choose Starter or a higher paid plan to avoid idle spin-down. Set the six Render environment variables listed above.
4. After Render provides its `onrender.com` URL, set `VITE_API_URL` to that origin in Vercel **before** the first frontend deployment.
5. Set Render's `CLIENT_URL` to the exact Vercel production origin. Add exact preview origins as comma-separated entries when needed. Do not use a wildcard; the backend checks each origin exactly.
6. Configure Vercel's `frontend` root and Vite build settings listed above, then deploy the frontend from Vercel when ready.
7. Check the Render health endpoint at `https://<render-service>.onrender.com/api/health`, then verify registration, poll creation, voting, duplicate-vote rejection, and live updates in the deployed site.

## Local development proxy

`VITE_DEV_API_TARGET` is optional and only configures Vite's `/api` and `/ws` proxy. Set it in `frontend/.env` to the backend origin used for local development. The Vite proxy is disabled when this variable is empty.
