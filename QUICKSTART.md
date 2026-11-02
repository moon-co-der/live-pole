# LivePoll

LivePoll is a small, responsive polling app. Poll creation and voting use Express APIs; Socket.IO broadcasts each accepted vote to all viewers in that poll's room. Polls and vote totals are stored in `data/polls.json` and survive server restarts.

## Run locally

1. Install Node.js 20 or newer.
2. From this project folder, run `npm install`.
3. Run `npm start`.
4. Open [http://localhost:3000/create](http://localhost:3000/create), create a question with 2–6 options, and share the generated poll link.

To run with automatic server restarts during development, use `npm run dev`.

## Deploy

Create a free Render **Web Service** from this repository. Set the build command to `npm install`, the start command to `npm start`, and the health check path to `/health`. The service listens on Render's `PORT` automatically. Railway can run the same commands.

The app uses a JSON file for simple durable local storage. Free Render and Railway instances may use ephemeral filesystems, so votes can be lost when an instance is replaced or restarted. For durable hosted polls, attach persistent storage at `/var/data` and set `DATA_DIR=/var/data`, or use a managed database. Render persistent disks require a paid service plan.

## Real-time flow

1. A browser joins a Socket.IO room named for its poll and receives the latest count snapshot.
2. A vote request sends the selected option and the browser's HttpOnly voter cookie to Express.
3. The server checks that the poll is open, the option is valid, and that this cookie has not already voted.
4. It records the vote to the JSON store, then broadcasts the complete updated results to that poll's Socket.IO room.
5. Every connected browser animates its bars and counts; reconnecting browsers automatically rejoin and receive a fresh snapshot.

The cookie prevents repeat votes from the same browser profile. It is a lightweight poll integrity check, not identity verification.
