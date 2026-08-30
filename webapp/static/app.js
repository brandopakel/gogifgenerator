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
  elements.searchMessage.textContent = 'Searching open media…';
  elements.searchTitle.textContent = `“${query}”`;

	const searches = [searchWikimedia(query)];
  const apiKey = state.config?.giphy_api_key;
	if (apiKey) searches.push(searchGiphy(query, apiKey));

	const settled = await Promise.allSettled(searches);
	let resultCount = 0;
	const failures = [];
	for (const outcome of settled) {
		if (outcome.status === 'rejected') {
			failures.push(outcome.reason.message);
			continue;
		}
		if (outcome.value.items.length) {
			resultCount += outcome.value.items.length;
			renderProvider(outcome.value);
		}
  }
	if (resultCount) {
		elements.searchMessage.hidden = true;
	} else {
		elements.searchMessage.hidden = false;
		elements.searchMessage.textContent = failures[0] || 'No matches yet. Try a broader feeling, action, or subject.';
	}
	elements.searchPanel.scrollIntoView({ behavior: 'smooth', block: 'start' });
}

async function searchWikimedia(query) {
	const url = new URL('/api/v1/providers/wikimedia/search', window.location.origin);
	url.search = new URLSearchParams({ q: query, limit: '18', locale: 'en' });
	const response = await fetch(url);
	const payload = await response.json().catch(() => ({}));
	if (!response.ok) throw new Error(payload.error?.message || 'Wikimedia Commons search failed.');
	return {
		label: 'Wikimedia Commons',
		credit: 'OPEN MEDIA · CHECK EACH LICENSE',
		items: payload.results.map((item) => ({
			href: item.source_url,
			preview: item.preview_url,
			title: item.title || query,
			note: [item.author, item.license_name].filter(Boolean).join(' · '),
		})),
	};
}

async function searchGiphy(query, apiKey) {
	const url = new URL('https://api.giphy.com/v1/gifs/search');
	url.search = new URLSearchParams({ api_key: apiKey, q: query, limit: '18', rating: 'pg-13', lang: 'en', bundle: 'messaging_non_clips' });
	const response = await fetch(url);
	const payload = await response.json().catch(() => ({}));
	if (!response.ok) throw new Error(payload.meta?.msg || 'GIPHY search failed.');
	return {
		label: 'GIPHY',
		credit: 'POWERED BY GIPHY',
		items: payload.data.map((item) => {
			const preview = item.images.fixed_width_small || item.images.fixed_width || item.images.original;
			return {
				href: item.url || item.images.original.url,
				preview: preview.webp || preview.url,
				title: item.title || query,
				note: item.user?.display_name || '',
			};
		}),
	};
}

function renderProvider(result) {
	const section = document.createElement('section');
	section.className = 'provider-section';
	const heading = document.createElement('div');
	heading.className = 'provider-heading';
	const title = document.createElement('h3');
	title.textContent = result.label;
	const credit = document.createElement('span');
	credit.textContent = result.credit;
	heading.append(title, credit);
	const grid = document.createElement('div');
	grid.className = 'search-grid';
	for (const item of result.items) grid.append(searchCard(item));
	section.append(heading, grid);
	elements.searchResults.append(section);
}

function searchCard(item) {
	const link = document.createElement('a');
	link.className = 'search-card';
	link.href = item.href;
	link.target = '_blank';
	link.rel = 'noreferrer';
	link.title = item.title;
	const image = document.createElement('img');
	image.src = item.preview;
	image.alt = item.title;
	image.loading = 'lazy';
	link.append(image);
	if (item.note) {
		const copy = document.createElement('span');
		copy.className = 'search-card-copy';
		copy.textContent = item.note;
		link.append(copy);
	}
	return link;
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
