# LivePoll

LivePoll is a live polling app. Owners create a poll and share its link; anyone with the link can vote, then watch results update in real time.

**Create a poll → Share the link → Collect votes → Watch live results**

## Features

### Poll owners

- Register and sign in with email and password.
- Create polls with 2–6 options, optional closing time, and single- or multiple-choice settings.
- Share polls with a link or QR code.
- Browse, search, sort, close, duplicate, and delete polls.
- See dashboard summaries and per-poll analytics, including votes over time, peak hours, device and source breakdowns.
- Receive live dashboard updates while votes arrive.

### Voters

- Vote from a public link without creating an account.
- See current results and live updates without refreshing.
- See when a poll is closed or has expired.

## Technology

| Area | Technology | Role |
|---|---|---|
| Frontend | React, Vite, React Router | Owner pages, public voting, and live results |
| Backend | Go, Gin | REST API, authentication, validation, and WebSockets |
| Database | MongoDB | Users, polls, votes, and views |
| Realtime and cache | Redis | Live counts, rate limits, duplicate-vote locks, pub/sub, tickets, and vote stream |

## How live voting works

1. A voter submits a vote to the Go API over HTTP.
2. The server validates the poll and option. A Redis Lua script checks the voter's one-year cookie lock and increments the selected option and total atomically.
3. The vote is queued in a Redis Stream for MongoDB persistence. The server publishes the new full count set through Redis pub/sub.
4. Each backend instance forwards updates to its connected WebSocket clients. A new connection receives a snapshot; clients reconnect and request a fresh snapshot after interruption.
5. A background reconciliation task compares active poll counts in Redis with MongoDB vote records and repairs mismatches.

The voter identifier is stored in an HttpOnly cookie. It helps prevent repeat votes from the same browser; it does not prove a person's identity across different browsers or devices.

## Project layout

```text
live-pole/
├── backend/
│   ├── cmd/
│   │   ├── seed/             # Local analytics demo data
│   │   └── server/           # API server entry point
│   └── internal/
│       ├── config/           # Environment and MongoDB setup
│       ├── handlers/         # HTTP and WebSocket handlers
│       ├── middleware/       # Auth, CORS, recovery, rate limits
│       ├── models/           # User, poll, vote, and view models
│       ├── realtime/         # WebSocket hub and clients
│       ├── redisx/           # Redis operations and Lua scripts
│       ├── repository/       # MongoDB queries
│       ├── services/         # Application logic and background work
│       └── validation/       # Request validation
├── frontend/
│   ├── public/               # Static assets, including the login illustration
│   └── src/
│       ├── api/              # API client
│       ├── components/       # Shared UI components
│       ├── context/          # Authentication state
│       ├── hooks/            # Live poll and dashboard WebSocket hooks
│       ├── pages/            # Application pages
│       ├── styles/           # Design tokens
│       └── utils/
├── docker-compose.yml        # Local MongoDB and Redis
└── README.md
```

Backend request flow: `route → middleware → handler → service → repository / Redis`.

See [DEPLOYMENT.md](DEPLOYMENT.md) for the Render, Atlas, Redis Cloud, and Vercel setup.

## Run locally

### Requirements

- Go 1.27.3 or newer (the Go module declares `go 1.27.3`).
- Node.js 20.19+ or 22.12+ (required by the Vite version in the lockfile).
- npm.
- Docker Desktop with Docker Compose, or local MongoDB and Redis services.

### 1. Start MongoDB and Redis

From the project root:

```powershell
docker compose up -d
```

The compose file starts MongoDB on `localhost:27017` and Redis on `localhost:6379`. Replace the fake database and Redis URIs in the example environment file with these local service addresses when running locally.

### 2. Configure and run the backend

From the project root, create the local environment file once:

```powershell
Copy-Item backend/.env.example backend/.env
```

Open `backend/.env` and replace the example `JWT_SECRET` with a private random value at least 32 characters long. Keep `.env` files out of commits.

In a terminal from the project root:

```powershell
cd backend
go mod download
go run ./cmd/server
```

The API listens at `http://localhost:5000`. Check backend and database health at [`http://localhost:5000/api/health`](http://localhost:5000/api/health).

### 3. Run the frontend

Open a second terminal from the project root:

```powershell
cd frontend
npm ci
npm run dev
```

Open [`http://localhost:5173`](http://localhost:5173). To proxy `/api` and `/ws` during local development, set `VITE_DEV_API_TARGET` in `frontend/.env` to your backend origin. Leave `VITE_API_URL` unset for the local proxy; production uses `VITE_API_URL` as its API origin.

### Environment variables

The backend loads `backend/.env` when started from the `backend` folder. These five values are required:

| Variable | Local example | Purpose |
|---|---|---|
| `PORT` | `5000` | HTTP and WebSocket server port |
| `MONGO_URI` | Fake placeholder in `.env.example` | MongoDB connection URI; the app uses the `live_polling` database |
| `REDIS_URL` | Fake TLS placeholder in `.env.example` | Redis TCP connection URL; use `rediss://` when TLS is required |
| `JWT_SECRET` | Replace the example with a random secret (32+ characters) | Signs and verifies owner JWTs |
| `CLIENT_URL` | Fake placeholder in `.env.example` | Exact allowed browser origin for CORS and WebSocket checks |

`COOKIE_SECURE` defaults to `false`; use `true` on the HTTPS production host. The optional WebSocket settings in `backend/.env.example` control ping/pong timing, write timeout, message size, and per-IP connection limits.

The frontend environment variable is:

| Variable | Default | Purpose |
|---|---|---|
| `VITE_API_URL` | Fake placeholder in `.env.example` | Backend API origin embedded at frontend build time |
| `VITE_DEV_API_TARGET` | Empty; proxy disabled | Optional local Vite proxy target for `/api` and `/ws` |

## Demo analytics data

To create the local analytics demo account and sample polls, start MongoDB and Redis, then run this from the backend folder:

```powershell
go run ./cmd/seed
```

Demo sign-in:

- Email: `analytics-demo@livepoll.local`
- Password: `LivePollDemo2026!`

**The seed script replaces the existing polls, votes, and views owned by this demo account each time it runs.** Do not use these credentials or seeded data in a public deployment.

## API overview

All errors use JSON with an `error` message. Owner routes require `Authorization: Bearer <token>` unless marked public.

| Method | Route | Access | Purpose |
|---|---|---|---|
| `GET` | `/api/health` | Public | MongoDB and Redis health |
| `POST` | `/api/auth/register` | Public | Create an account |
| `POST` | `/api/auth/login` | Public | Sign in and receive a JWT |
| `GET` | `/api/auth/me` | Owner | Get the signed-in user |
| `POST` | `/api/polls` | Owner | Create a poll |
| `GET` | `/api/polls` | Owner | List and filter owned polls |
| `GET` | `/api/polls/:id` | Owner | Get an owned poll |
| `PATCH` | `/api/polls/:id/close` | Owner | Close an owned poll |
| `DELETE` | `/api/polls/:id` | Owner | Delete a poll and its related data |
| `GET` | `/api/analytics/overview` | Owner | Overview totals and growth |
| `GET` | `/api/analytics/votes-over-time?days=7` | Owner | Daily votes (`days` can be `7` or `30`) |
| `GET` | `/api/analytics/top-polls` | Owner | Top polls by votes |
| `GET` | `/api/polls/:id/analytics` | Owner | Analytics for an owned poll |
| `POST` | `/api/ws/ticket` | Owner | Create a one-use dashboard WebSocket ticket |
| `GET` | `/api/public/polls/:slug` | Public | Get public poll details and set voter cookie |
| `POST` | `/api/public/polls/:slug/vote` | Public | Submit a vote |
| `GET` | `/ws/polls/:slug` | Public WebSocket | Poll snapshot and live results |
| `GET` | `/ws/dashboard?ticket=...` | Ticket WebSocket | Owner dashboard live updates |

Rate limits apply to registration, login, public poll reads, votes, and WebSocket connections. Limited requests return `429` with `Retry-After`.

## Tests and checks

Run backend tests from `backend/`:

```powershell
go test ./...
```

Check that the frontend builds from `frontend/`:

```powershell
npm run build
```

Manual end-to-end checks:

1. Register, log in, refresh, and confirm the owner session remains available.
2. Create a poll, copy its share link, and open it in a private browser window.
3. Vote once, then try voting again with the same browser cookie; the second vote should be rejected.
4. Open the poll in another window and confirm its results update without refreshing.
5. Close the poll from the owner page and confirm viewers see the closed state and cannot vote again.
6. For cross-instance realtime, run a second backend on another port against the same MongoDB and Redis; connect a WebSocket to one instance and cast a vote through the other.

## Production checklist

- Use HTTPS and `wss://`; enable secure voter cookies with `COOKIE_SECURE=true`.
- Set `CLIENT_URL` to the exact deployed frontend origin.
- Provide a strong private `JWT_SECRET` through the hosting environment, never source control.
- Provision persistent MongoDB and Redis services and configure their connection URLs.
- Configure the frontend host to serve `index.html` for client-side routes such as `/p/:slug`.
- Confirm the backend host supports long-lived WebSocket connections.
- Do not run the local demo seed script against production data.

## AI assistance

OpenAI Codex assisted with implementation and documentation. The project source and the checks above should be reviewed and run in the target environment before release.
