const elements = {
  form: document.querySelector('#prompt-form'),
  prompt: document.querySelector('#prompt'),
  submit: document.querySelector('#submit-button'),
  submitLabel: document.querySelector('#submit-label'),
  modes: [...document.querySelectorAll('.mode')],
  createPanel: document.querySelector('#create-panel'),
  searchPanel: document.querySelector('#search-panel'),
  preview: document.querySelector('#gif-preview'),
  previewEmpty: document.querySelector('#preview-empty'),
  working: document.querySelector('#working'),
  resultTitle: document.querySelector('#result-title'),
  resultNote: document.querySelector('#result-note'),
  download: document.querySelector('#download-button'),
  reroll: document.querySelector('#reroll-button'),
  size: document.querySelector('#size-control'),
  tempo: document.querySelector('#tempo-control'),
  engine: document.querySelector('#engine-badge'),
  searchTitle: document.querySelector('#search-title'),
  searchMessage: document.querySelector('#search-message'),
  searchResults: document.querySelector('#search-results'),
  giphyCredit: document.querySelector('#giphy-credit'),
  install: document.querySelector('#install-button'),
  toast: document.querySelector('#toast'),
};

const state = { mode: 'create', config: null, objectURL: '', seed: 0, installPrompt: null };

async function loadConfig() {
  try {
    const response = await fetch('/api/v1/config');
    state.config = await response.json();
    if (state.config.planner === 'ai') {
      elements.engine.innerHTML = '<span></span> AI art director';
      elements.resultNote.textContent = `AI-directed with ${state.config.model}; rendered locally by Go.`;
    }
  } catch {
    showToast('Could not read app configuration.');
  }
}

function setMode(mode) {
  state.mode = mode;
  for (const button of elements.modes) {
    const active = button.dataset.mode === mode;
    button.classList.toggle('active', active);
    button.setAttribute('aria-selected', String(active));
  }
  const creating = mode === 'create';
  elements.createPanel.hidden = !creating;
  elements.searchPanel.hidden = creating;
  elements.submitLabel.textContent = creating ? 'Make it' : 'Find it';
  elements.submit.setAttribute('aria-label', creating ? 'Create GIF' : 'Search GIFs');
  elements.prompt.placeholder = creating ? 'A tiny victory dance after shipping...' : 'Search reactions, moods, moments...';
}

async function submitPrompt(event) {
  event.preventDefault();
  const prompt = elements.prompt.value.trim();
  if (!prompt) return;
  if (state.mode === 'create') await generate(prompt);
  else await search(prompt);
}

async function generate(prompt, reroll = false) {
  setWorking(true);
  if (reroll) state.seed = Date.now();
  try {
    const size = Number(elements.size.value);
    const response = await fetch('/api/v1/gifs/generate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        prompt,
        width: size,
        height: size,
        frames: 18,
        delay_ms: Number(elements.tempo.value),
        seed: state.seed,
      }),
    });
    if (!response.ok) {
      const problem = await response.json().catch(() => ({}));
      throw new Error(problem.error?.message || `Generation failed (${response.status})`);
    }
    const blob = await response.blob();
    if (state.objectURL) URL.revokeObjectURL(state.objectURL);
    state.objectURL = URL.createObjectURL(blob);
    elements.preview.src = state.objectURL;
    elements.preview.hidden = false;
    elements.previewEmpty.hidden = true;
    elements.resultTitle.textContent = prompt.length > 48 ? `${prompt.slice(0, 48)}…` : prompt;
    const engine = response.headers.get('X-GoGIF-Engine') || 'local';
    elements.resultNote.textContent = engine.startsWith('openai')
      ? 'AI-directed, then rendered frame by frame by the Go engine.'
      : engine.includes('fallback')
        ? 'The AI planner was unavailable, so the local art director kept things moving.'
        : 'Planned and rendered locally by the Go engine—fast, private, and repeatable.';
    elements.download.href = state.objectURL;
    elements.download.classList.remove('disabled');
    elements.download.setAttribute('aria-disabled', 'false');
    elements.reroll.disabled = false;
    elements.createPanel.scrollIntoView({ behavior: 'smooth', block: 'start' });
  } catch (error) {
    showToast(error.message);
  } finally {
    setWorking(false);
  }
}

async function search(query) {
  elements.searchResults.replaceChildren();
  elements.searchMessage.hidden = false;
  elements.searchMessage.textContent = 'Searching the GIFverse…';
  elements.searchTitle.textContent = `“${query}”`;
  const apiKey = state.config?.giphy_api_key;
  if (!apiKey) {
    elements.searchMessage.textContent = 'Add GIPHY_API_KEY to the server environment to enable licensed catalog search.';
    return;
  }
  try {
    const url = new URL('https://api.giphy.com/v1/gifs/search');
    url.search = new URLSearchParams({ api_key: apiKey, q: query, limit: '24', rating: 'pg-13', lang: 'en', bundle: 'messaging_non_clips' });
    const response = await fetch(url);
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.meta?.msg || 'GIF search failed.');
    elements.giphyCredit.hidden = false;
    elements.searchMessage.hidden = payload.data.length > 0;
    if (!payload.data.length) elements.searchMessage.textContent = 'No matches yet. Try a broader feeling or reaction.';
    for (const item of payload.data) {
      const preview = item.images.fixed_width_small || item.images.fixed_width || item.images.original;
      const original = item.images.original;
      const link = document.createElement('a');
      link.className = 'search-card';
      link.href = original.url;
      link.target = '_blank';
      link.rel = 'noreferrer';
      link.title = item.title || query;
      const image = document.createElement('img');
      image.src = preview.webp || preview.url;
      image.alt = item.title || `${query} GIF`;
      image.loading = 'lazy';
      link.append(image);
      elements.searchResults.append(link);
    }
    elements.searchPanel.scrollIntoView({ behavior: 'smooth', block: 'start' });
  } catch (error) {
    elements.searchMessage.hidden = false;
    elements.searchMessage.textContent = error.message;
  }
}

function setWorking(working) {
  elements.submit.disabled = working;
  elements.working.hidden = !working;
}

let toastTimer;
function showToast(message) {
  clearTimeout(toastTimer);
  elements.toast.textContent = message;
  elements.toast.hidden = false;
  toastTimer = setTimeout(() => { elements.toast.hidden = true; }, 3500);
}

for (const mode of elements.modes) mode.addEventListener('click', () => setMode(mode.dataset.mode));
for (const suggestion of document.querySelectorAll('[data-prompt]')) {
  suggestion.addEventListener('click', () => {
    elements.prompt.value = suggestion.dataset.prompt;
    elements.prompt.focus();
  });
}
elements.form.addEventListener('submit', submitPrompt);
elements.reroll.addEventListener('click', () => generate(elements.prompt.value.trim(), true));
elements.prompt.addEventListener('input', () => {
  elements.prompt.style.height = 'auto';
  elements.prompt.style.height = `${Math.min(elements.prompt.scrollHeight, 130)}px`;
});

window.addEventListener('beforeinstallprompt', (event) => {
  event.preventDefault();
  state.installPrompt = event;
  elements.install.hidden = false;
});
elements.install.addEventListener('click', async () => {
  if (!state.installPrompt) return;
  await state.installPrompt.prompt();
  state.installPrompt = null;
  elements.install.hidden = true;
});

if ('serviceWorker' in navigator) navigator.serviceWorker.register('/service-worker.js');
loadConfig();
