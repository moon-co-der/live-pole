# Run LivePoll locally

This React app signs in through the Go API in `backend/`. The root `npm start` command runs an older Express demo and does not provide the sign-in API.

## 1. Start MongoDB and Redis

Start Docker Desktop, then from the project root:

```powershell
docker compose up -d
```

If you do not use Docker, run local MongoDB on port `27017` and Redis on port `6379`.

## 2. Start the Go backend

`backend/.env` should point to the local database services and use `CLIENT_URL=http://localhost:5173`. From the project root:

```powershell
cd backend
go run ./cmd/server
```

The API should be available at `http://localhost:5000/api/health`.

## 3. Start the React frontend

Open another terminal from the project root:

```powershell
cd frontend
npm install
npm run dev
```

Open `http://localhost:5173`. The development server proxies API and WebSocket requests to `http://localhost:5000` by default. Set `VITE_DEV_API_TARGET` in `frontend/.env` if your backend uses a different address.

Create an account at `/signup`, then sign in at `/login`. See `README.md` for environment setup and deployment details.
