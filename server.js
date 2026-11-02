'use strict';

const express = require('express');
const http = require('node:http');
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const { Server } = require('socket.io');

const app = express();
const server = http.createServer(app);
const io = new Server(server, { cors: { origin: false } });
const PORT = Number(process.env.PORT || 3000);
const DATA_DIR = process.env.DATA_DIR || path.join(__dirname, 'data');
const DB_FILE = path.join(DATA_DIR, 'polls.json');
const rate = new Map();

fs.mkdirSync(DATA_DIR, { recursive: true });
if (!fs.existsSync(DB_FILE)) fs.writeFileSync(DB_FILE, JSON.stringify({ polls: {} }, null, 2));
let db = JSON.parse(fs.readFileSync(DB_FILE, 'utf8'));
// Serialize writes so concurrent votes cannot overwrite each other.
let writeQueue = Promise.resolve();
function save() {
  const snapshot = JSON.stringify(db, null, 2);
  writeQueue = writeQueue.then(async () => {
    const temp = `${DB_FILE}.${process.pid}.tmp`;
    await fs.promises.writeFile(temp, snapshot, { mode: 0o600 });
    await fs.promises.rename(temp, DB_FILE);
  });
  return writeQueue;
}
function id() { return crypto.randomBytes(8).toString('hex'); }
function hash(value) { return crypto.createHash('sha256').update(value).digest('hex'); }
function counts(poll) {
  const votes = poll.options.map((o) => ({ id: o.id, text: o.text, votes: o.votes }));
  const totalVotes = votes.reduce((sum, o) => sum + o.votes, 0);
  return { options: votes.map((o) => ({ ...o, percentage: totalVotes ? Math.round(o.votes / totalVotes * 100) : 0 })), totalVotes };
}
function publicPoll(poll) { return { id: poll.id, question: poll.question, options: poll.options.map(({ id, text }) => ({ id, text })), closed: poll.closed }; }
function rateLimit(req, res, next) {
  const key = req.ip;
  const now = Date.now();
  const hits = (rate.get(key) || []).filter((time) => now - time < 60_000);
  if (hits.length >= 40) return res.status(429).json({ error: 'Too many requests. Please try again in a minute.' });
  hits.push(now); rate.set(key, hits); next();
}
app.disable('x-powered-by');
app.get('/', (_req, res) => res.redirect(302, '/create'));
app.use(express.json({ limit: '12kb' }));
// The HttpOnly cookie is the server-verifiable device key. localStorage on the
// client is only a convenience for instantly restoring the results view.
app.use((req, res, next) => {
  const cookie = req.headers.cookie || '';
  const existing = cookie.match(/(?:^|;\s*)lpv=([a-f0-9]{48})(?:;|$)/)?.[1];
  req.voterCookie = existing || crypto.randomBytes(24).toString('hex');
  if (!existing) res.setHeader('Set-Cookie', `lpv=${req.voterCookie}; HttpOnly; SameSite=Lax; Path=/; Max-Age=31536000${process.env.NODE_ENV === 'production' ? '; Secure' : ''}`);
  next();
});
app.use('/api', rateLimit);
app.use(express.static(path.join(__dirname, 'public'), { etag: true, maxAge: '1h' }));

app.get('/api/polls/:id', (req, res) => {
  const poll = db.polls[req.params.id];
  if (!poll) return res.status(404).json({ error: 'Poll not found.' });
  const fingerprint = hash(`${req.voterCookie}:${req.get('user-agent') || ''}`);
  res.json({ poll: publicPoll(poll), results: counts(poll), hasVoted: Boolean(fingerprint && poll.voters[fingerprint]) });
});

app.post('/api/polls', async (req, res) => {
  const question = typeof req.body.question === 'string' ? req.body.question.trim() : '';
  const options = Array.isArray(req.body.options) ? req.body.options.map((x) => typeof x === 'string' ? x.trim() : '').filter(Boolean) : [];
  if (question.length < 4 || question.length > 180) return res.status(400).json({ error: 'Question must be 4–180 characters.' });
  if (options.length < 2 || options.length > 6 || options.some((x) => x.length > 100)) return res.status(400).json({ error: 'Add 2–6 options, each up to 100 characters.' });
  const pollId = id();
  const adminToken = crypto.randomBytes(24).toString('hex');
  db.polls[pollId] = { id: pollId, question, options: options.map((text) => ({ id: id(), text, votes: 0 })), voters: {}, adminHash: hash(adminToken), closed: false, createdAt: new Date().toISOString() };
  await save();
  res.status(201).json({ poll: publicPoll(db.polls[pollId]), adminToken });
});

app.post('/api/polls/:id/vote', async (req, res) => {
  const poll = db.polls[req.params.id];
  if (!poll) return res.status(404).json({ error: 'Poll not found.' });
  if (poll.closed) return res.status(409).json({ error: 'This poll is closed.' });
  const option = poll.options.find((x) => x.id === req.body.optionId);
  if (!option) return res.status(400).json({ error: 'Choose a valid option.' });
  const fingerprint = hash(`${req.voterCookie}:${req.get('user-agent') || ''}`);
  if (poll.voters[fingerprint]) return res.status(409).json({ error: 'This device has already voted.', results: counts(poll) });
  poll.voters[fingerprint] = option.id;
  option.votes += 1;
  await save();
  const results = counts(poll);
  io.to(poll.id).emit('results', results);
  res.json({ results });
});

app.post('/api/polls/:id/close', async (req, res) => {
  const poll = db.polls[req.params.id];
  if (!poll) return res.status(404).json({ error: 'Poll not found.' });
  if (!req.body.adminToken || hash(req.body.adminToken) !== poll.adminHash) return res.status(403).json({ error: 'Invalid admin link.' });
  poll.closed = true;
  await save();
  io.to(poll.id).emit('closed');
  res.json({ closed: true });
});

io.on('connection', (socket) => {
  socket.on('join-poll', (pollId) => {
    if (typeof pollId !== 'string' || pollId.length > 40) return;
    const poll = db.polls[pollId];
    if (!poll) return;
    socket.join(pollId);
    socket.emit('results', counts(poll));
    if (poll.closed) socket.emit('closed');
  });
});

app.get('/poll/:id', (_req, res) => res.sendFile(path.join(__dirname, 'public', 'index.html')));
app.get('/create', (_req, res) => res.sendFile(path.join(__dirname, 'public', 'create.html')));
app.get('/health', (_req, res) => res.json({ ok: true }));
app.use((err, _req, res, _next) => {
  console.error(err);
  res.status(err.status || 500).json({ error: err.status === 413 ? 'Request is too large.' : 'Something went wrong. Please try again.' });
});

server.listen(PORT, '0.0.0.0', () => console.log(`LivePoll listening on port ${PORT}`));
