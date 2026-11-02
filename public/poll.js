(() => {
  const pollId = decodeURIComponent(location.pathname.split('/').filter(Boolean).pop() || '');
  const question = document.querySelector('#question');
  const content = document.querySelector('#content');
  const error = document.querySelector('#error');
  const status = document.querySelector('#status');
  const dot = status.querySelector('.dot');
  const voterKey = `livepoll-voter-${pollId}`;
  let poll; let results; let hasVoted = false; let showResults = false;
  const api = async (url, options = {}) => fetch(url, { ...options, credentials: 'same-origin', headers: { 'Content-Type': 'application/json', ...options.headers } });
  function connection(connected) {
    dot.classList.toggle('offline', !connected);
    status.querySelector('span').textContent = connected ? (poll?.closed ? 'POLL CLOSED' : hasVoted || showResults ? 'LIVE RESULTS' : 'LIVE POLL') : 'RECONNECTING…';
    status.classList.toggle('offline-text', !connected);
  }
  function render() {
    if (!poll) return;
    const reveal = hasVoted || showResults || poll.closed;
    status.querySelector('span').textContent = poll.closed ? 'POLL CLOSED' : reveal ? 'LIVE RESULTS' : 'LIVE POLL';
    content.replaceChildren();
    if (reveal) {
      const list = document.createElement('div'); list.className = 'results'; list.setAttribute('aria-label', 'Live poll results');
      for (const option of results.options) {
        const row = document.createElement('div');
        const head = document.createElement('div'); head.className = 'result-head';
        const label = document.createElement('span'); label.textContent = option.text;
        const value = document.createElement('strong'); value.textContent = `${option.votes} · ${option.percentage}%`;
        const track = document.createElement('div'); track.className = 'track'; track.setAttribute('role', 'progressbar'); track.setAttribute('aria-label', `${option.text}: ${option.percentage}%`); track.setAttribute('aria-valuenow', option.percentage); track.setAttribute('aria-valuemin', 0); track.setAttribute('aria-valuemax', 100);
        const bar = document.createElement('div'); bar.className = 'bar'; bar.style.width = `${option.percentage}%`; track.append(bar); head.append(label, value); row.append(head, track); list.append(row);
      }
      const total = document.createElement('div'); total.className = 'total'; total.innerHTML = '<span>Total votes</span>'; const totalValue = document.createElement('strong'); totalValue.textContent = results.totalVotes; total.append(totalValue); content.append(list, total);
    } else {
      const form = document.createElement('form'); const hint = document.createElement('p'); hint.className = 'hint'; hint.textContent = 'Choose one option'; const fieldset = document.createElement('fieldset'); fieldset.className = 'options'; fieldset.setAttribute('aria-label', 'Choose one option');
      for (const option of poll.options) { const label = document.createElement('label'); label.className = 'option'; const input = document.createElement('input'); input.type = 'radio'; input.name = 'option'; input.value = option.id; input.setAttribute('aria-label', option.text); const text = document.createElement('span'); text.textContent = option.text; input.addEventListener('change', () => { fieldset.querySelectorAll('.option').forEach((x) => x.classList.remove('selected')); label.classList.add('selected'); button.disabled = false; }); label.append(input, text); fieldset.append(label); }
      const button = document.createElement('button'); button.className = 'vote'; button.type = 'submit'; button.disabled = true; button.textContent = 'Vote';
      form.addEventListener('submit', async (event) => { event.preventDefault(); const choice = new FormData(form).get('option'); if (!choice || button.disabled) return; button.disabled = true; button.textContent = 'Submitting…'; error.textContent = ''; try { const response = await api(`/api/polls/${encodeURIComponent(pollId)}/vote`, { method: 'POST', body: JSON.stringify({ optionId: choice }) }); const data = await response.json(); if (!response.ok) throw new Error(data.error || 'Unable to vote.'); results = data.results; hasVoted = true; showResults = true; localStorage.setItem(`${voterKey}-voted`, '1'); render(); } catch (e) { error.textContent = e.message; button.disabled = false; button.textContent = 'Vote'; } });
      form.append(hint, fieldset, button); const view = document.createElement('button'); view.className = 'view-results'; view.type = 'button'; view.textContent = 'View results'; view.addEventListener('click', () => { showResults = true; render(); }); content.append(form, view);
    }
  }
  async function load() {
    try { const response = await api(`/api/polls/${encodeURIComponent(pollId)}`); const data = await response.json(); if (!response.ok) throw new Error(data.error); poll = data.poll; results = data.results; hasVoted = data.hasVoted || localStorage.getItem(`${voterKey}-voted`) === '1'; question.textContent = poll.question; render(); }
    catch (e) { question.textContent = 'Poll unavailable'; error.textContent = e.message || 'This poll could not be loaded.'; }
  }
  load();
  const socket = io({ reconnection: true, reconnectionDelayMax: 5000 });
  socket.on('connect', () => { connection(true); socket.emit('join-poll', pollId); });
  socket.on('disconnect', () => connection(false));
  socket.on('connect_error', () => connection(false));
  socket.on('results', (next) => { results = next; if (poll) render(); });
  socket.on('closed', () => { if (poll) { poll.closed = true; render(); } });
})();
