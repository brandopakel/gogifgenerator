const elements = {
  form: document.querySelector('#prompt-form'),
  prompt: document.querySelector('#prompt'),
  submit: document.querySelector('#submit-button'),
  submitLabel: document.querySelector('#submit-label'),
  modes: [...document.querySelectorAll('.mode')],
  createPanel: document.querySelector('#create-panel'),
  searchPanel: document.querySelector('#search-panel'),
  previewShell: document.querySelector('#preview-shell'),
  preview: document.querySelector('#gif-preview'),
  videoPreview: document.querySelector('#video-preview'),
  editorOverlay: document.querySelector('#editor-overlay'),
  captionGuide: document.querySelector('#caption-guide'),
  previewEmpty: document.querySelector('#preview-empty'),
  working: document.querySelector('#working'),
  resultTitle: document.querySelector('#result-title'),
  download: document.querySelector('#download-button'),
  reroll: document.querySelector('#reroll-button'),
  copy: document.querySelector('#copy-button'),
  share: document.querySelector('#share-button'),
  presetField: document.querySelector('#preset-field'),
  preset: document.querySelector('#preset-control'),
  size: document.querySelector('#size-control'),
  tempo: document.querySelector('#tempo-control'),
  quality: document.querySelector('#quality-control'),
  targetSizeField: document.querySelector('#target-size-field'),
  targetSize: document.querySelector('#target-size-control'),
  engine: document.querySelector('#engine-badge'),
  searchTitle: document.querySelector('#search-title'),
  searchMessage: document.querySelector('#search-message'),
  searchResults: document.querySelector('#search-results'),
  searchOptions: document.querySelector('#search-options'),
  searchScope: document.querySelector('#search-scope'),
  install: document.querySelector('#install-button'),
  referenceChip: document.querySelector('#reference-chip'),
  referenceLabel: document.querySelector('#reference-label'),
  referenceClear: document.querySelector('#reference-clear'),
  suggestions: document.querySelector('.suggestions'),
  uploadEditor: document.querySelector('#upload-editor'),
  uploadMedia: document.querySelector('#upload-media'),
  uploadLabel: document.querySelector('#upload-label'),
  videoCapability: document.querySelector('#video-capability'),
  editControls: document.querySelector('#edit-controls'),
  editorGrid: document.querySelector('.editor-grid'),
  captionPosition: document.querySelector('#caption-position-control'),
  motion: document.querySelector('#motion-control'),
  cropX: document.querySelector('#crop-x-control'),
  cropY: document.querySelector('#crop-y-control'),
  zoom: document.querySelector('#zoom-control'),
  cropXOutput: document.querySelector('#crop-x-output'),
  cropYOutput: document.querySelector('#crop-y-output'),
  zoomOutput: document.querySelector('#zoom-output'),
  trimStart: document.querySelector('#trim-start-control'),
  trimEnd: document.querySelector('#trim-end-control'),
  loop: document.querySelector('#loop-control'),
  undo: document.querySelector('#undo-button'),
  redo: document.querySelector('#redo-button'),
  saveDraft: document.querySelector('#save-draft-button'),
  drafts: document.querySelector('#draft-control'),
  loadDraft: document.querySelector('#load-draft-button'),
  deleteDraft: document.querySelector('#delete-draft-button'),
  toast: document.querySelector('#toast'),
};

const state = {
  mode: 'create', config: null, objectURL: '', uploadPreviewURL: '', resultBlob: null,
  uploadFile: null, seed: 0, installPrompt: null, reference: null, uploadIsVideo: false,
  history: [], historyIndex: -1, applyingHistory: false, currentDraftID: '', drag: null,
  searchRequestID: 0,
};

const prefersReducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
const editorControlKeys = ['captionPosition', 'motion', 'cropX', 'cropY', 'zoom', 'trimStart', 'trimEnd', 'loop', 'size', 'tempo', 'quality', 'targetSize'];
let searchTimer;

async function loadConfig() {
  try {
    const response = await fetch('/api/v1/config');
    state.config = await response.json();
    if (state.config.planner === 'ai') {
      elements.engine.innerHTML = '<span></span> AI art director';
    }
    if (state.config.image_generator?.local) {
      elements.engine.innerHTML = '<span></span> Local generative engine';
    }
    if (state.config.video_editor?.enabled) {
      elements.videoCapability.hidden = true;
    } else {
      elements.videoCapability.textContent = 'Video requires FFmpeg.';
      elements.videoCapability.hidden = false;
    }
    updateSearchScope();
    if (state.mode === 'search') queueSearch();
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
  const searching = mode === 'search';
  const editing = mode === 'edit';
  elements.createPanel.hidden = searching;
  elements.searchPanel.hidden = !searching;
  elements.searchOptions.hidden = !searching;
  elements.uploadEditor.hidden = !editing;
  elements.editControls.hidden = !editing;
  elements.suggestions.hidden = editing;
  elements.referenceChip.hidden = editing || !state.reference;
  elements.prompt.required = !editing;
  elements.submitLabel.textContent = editing ? 'Export GIF' : searching ? 'Find it' : 'Make it';
  elements.submit.setAttribute('aria-label', editing ? 'Export edited GIF' : searching ? 'Search GIFs' : 'Create GIF');
  elements.prompt.placeholder = editing ? 'Add a caption (optional)…' : searching ? 'Search reactions, moods, moments...' : 'A tiny victory dance after shipping...';
  elements.reroll.textContent = editing ? 'Re-export' : 'Reroll';
  elements.presetField.hidden = !editing;
  elements.targetSizeField.hidden = !editing;
  elements.prompt.maxLength = editing ? 42 : 500;
  elements.previewShell.classList.toggle('editing', editing && Boolean(state.uploadFile) && !state.resultBlob);
  elements.editorOverlay.hidden = !(editing && state.uploadFile && !state.resultBlob);
  if (editing && state.history.length === 0) recordEditorState();
  if (searching) queueSearch();
  else {
    clearTimeout(searchTimer);
    state.searchRequestID += 1;
  }
}

function scrollToElement(element) {
  element.scrollIntoView({ behavior: prefersReducedMotion.matches ? 'auto' : 'smooth', block: 'start' });
}

async function submitPrompt(event) {
  event.preventDefault();
  const prompt = elements.prompt.value.trim();
  if (state.mode === 'edit') {
    await exportUpload(prompt);
  } else if (prompt && state.mode === 'create') {
    await generate(prompt);
  } else if (prompt) {
    await search(prompt);
  }
}

async function generate(prompt, reroll = false) {
  setWorking(true);
  if (reroll) state.seed = Date.now();
  try {
    const size = Number(elements.size.value);
    const payload = {
      prompt,
      width: size,
      height: size,
      frames: Number(elements.quality.value),
      delay_ms: Number(elements.tempo.value),
      seed: state.seed,
    };
    let endpoint = '/api/v1/gifs/generate';
    if (state.reference) {
      endpoint = '/api/v1/gifs/generate-from-reference';
      payload.provider = state.reference.provider;
      payload.external_id = state.reference.externalID;
      payload.locale = 'en';
    }
    const response = await fetch(endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!response.ok) {
      const problem = await response.json().catch(() => ({}));
      throw new Error(problem.error?.message || `Generation failed (${response.status})`);
    }
    const blob = await response.blob();
    presentResult(blob);
    elements.resultTitle.textContent = prompt.length > 48 ? `${prompt.slice(0, 48)}…` : prompt;
    elements.reroll.disabled = false;
    scrollToElement(elements.createPanel);
  } catch (error) {
    showToast(error.message);
  } finally {
    setWorking(false);
  }
}

function selectUpload() {
  const [file] = elements.uploadMedia.files;
  if (!file) return;
  if (file.size > 20 * 1024 * 1024) {
    elements.uploadMedia.value = '';
    showToast('Choose a file no larger than 20 MiB.');
    return;
  }
  loadUploadFile(file);
}

function loadUploadFile(file, keepSettings = false) {
  clearResult(false);
  state.uploadFile = file;
  state.uploadIsVideo = file.type.startsWith('video/') || /\.(mp4|mov|m4v|webm)$/i.test(file.name);
  state.currentDraftID = '';
  if (state.uploadPreviewURL) URL.revokeObjectURL(state.uploadPreviewURL);
  state.uploadPreviewURL = URL.createObjectURL(file);
  elements.preview.hidden = state.uploadIsVideo;
  elements.videoPreview.hidden = !state.uploadIsVideo;
  if (state.uploadIsVideo) {
    elements.videoPreview.src = state.uploadPreviewURL;
    elements.videoPreview.addEventListener('loadedmetadata', () => {
      if (!Number.isFinite(elements.videoPreview.duration)) return;
      elements.trimEnd.max = String(Math.min(315, elements.videoPreview.duration));
      if (!keepSettings) elements.trimEnd.value = String(Math.min(3, elements.videoPreview.duration).toFixed(1));
    }, { once: true });
  } else {
    elements.preview.src = state.uploadPreviewURL;
    elements.videoPreview.removeAttribute('src');
    elements.videoPreview.load();
  }
  elements.previewEmpty.hidden = true;
  elements.uploadLabel.textContent = file.name;
  elements.resultTitle.textContent = file.name;
  elements.editorGrid.classList.toggle('has-video', state.uploadIsVideo);
  elements.editorOverlay.hidden = false;
  elements.previewShell.classList.add('editing');
  if (!keepSettings) {
    state.history = [];
    state.historyIndex = -1;
    recordEditorState();
  }
  updateEditorVisuals();
  scrollToElement(elements.createPanel);
}

async function exportUpload(caption) {
  if (!state.uploadFile) {
    showToast('Choose a photo, GIF, or short video first.');
    elements.uploadMedia.click();
    return;
  }
  const trimStart = Number(elements.trimStart.value);
  const trimEnd = Number(elements.trimEnd.value);
  if (state.uploadIsVideo && (!Number.isFinite(trimStart) || !Number.isFinite(trimEnd) || trimStart < 0 || trimEnd <= trimStart || trimEnd - trimStart > 15)) {
    showToast('Choose a positive video trim no longer than 15 seconds.');
    elements.trimEnd.focus();
    return;
  }
  setWorking(true);
  try {
    const size = Number(elements.size.value);
    const body = new FormData();
    body.append('media', state.uploadFile, state.uploadFile.name);
    body.append('caption', caption);
    body.append('width', String(size));
    body.append('height', String(size));
    body.append('frames', elements.quality.value);
    body.append('delay_ms', elements.tempo.value);
    body.append('motion', elements.motion.value);
    body.append('seed', String(Date.now()));
    body.append('crop_x', elements.cropX.value);
    body.append('crop_y', elements.cropY.value);
    body.append('zoom', elements.zoom.value);
    body.append('caption_position', elements.captionPosition.value);
    body.append('loop', String(elements.loop.checked));
    body.append('trim_start_ms', String(Math.round(trimStart * 1000)));
    body.append('trim_end_ms', String(Math.round(trimEnd * 1000)));
    body.append('max_bytes', elements.targetSize.value);
    const response = await fetch('/api/v1/gifs/generate-from-upload', { method: 'POST', body });
    if (!response.ok) {
      const problem = await response.json().catch(() => ({}));
      throw new Error(problem.error?.message || `Upload export failed (${response.status})`);
    }
    const blob = await response.blob();
    presentResult(blob);
    elements.resultTitle.textContent = caption || state.uploadFile.name;
    elements.reroll.disabled = false;
    scrollToElement(elements.createPanel);
  } catch (error) {
    showToast(error.message);
  } finally {
    setWorking(false);
  }
}

function presentResult(blob) {
  if (state.objectURL) URL.revokeObjectURL(state.objectURL);
  state.resultBlob = blob;
  state.objectURL = URL.createObjectURL(blob);
  elements.preview.src = state.objectURL;
  elements.preview.hidden = false;
  elements.preview.style.transform = '';
  elements.videoPreview.hidden = true;
  elements.videoPreview.pause();
  elements.editorOverlay.hidden = true;
  elements.previewShell.classList.remove('editing', 'dragging');
  elements.previewShell.classList.add('has-result');
  elements.preview.setAttribute('aria-label', 'Generated GIF. On iPhone, touch and hold to copy or save it.');
  elements.previewEmpty.hidden = true;
  elements.download.href = state.objectURL;
  elements.download.classList.remove('disabled');
  elements.download.setAttribute('aria-disabled', 'false');
  elements.share.disabled = false;
  elements.copy.disabled = false;
}

function clearResult(restoreUpload = true) {
  if (state.objectURL) URL.revokeObjectURL(state.objectURL);
  state.objectURL = '';
  state.resultBlob = null;
  elements.previewShell.classList.remove('has-result');
  elements.preview.setAttribute('aria-label', 'GIF preview');
  elements.download.removeAttribute('href');
  elements.download.classList.add('disabled');
  elements.download.setAttribute('aria-disabled', 'true');
  elements.share.disabled = true;
  elements.copy.disabled = true;
  elements.reroll.disabled = true;
  if (restoreUpload && state.uploadFile && state.mode === 'edit') {
    elements.preview.src = state.uploadIsVideo ? '' : state.uploadPreviewURL;
    elements.preview.hidden = state.uploadIsVideo;
    elements.videoPreview.hidden = !state.uploadIsVideo;
    elements.editorOverlay.hidden = false;
    elements.previewShell.classList.add('editing');
    updateEditorVisuals();
  }
}

async function shareResult() {
  if (!state.resultBlob) return;
  const file = new File([state.resultBlob], 'gogif.gif', { type: 'image/gif' });
  const shareData = { files: [file], title: 'GoGIF', text: elements.resultTitle.textContent };
  if (!navigator.share || (navigator.canShare && !navigator.canShare(shareData))) {
    showToast('File sharing is unavailable here. Use Download instead.');
    return;
  }
  try {
    await navigator.share(shareData);
  } catch (error) {
    if (error.name !== 'AbortError') showToast(error.message);
  }
}

async function copyResult() {
  if (!state.resultBlob) return;
  if (!navigator.clipboard?.write || typeof ClipboardItem === 'undefined') {
    showToast('GIF clipboard writing is unavailable here. Use Share or Download instead.');
    return;
  }
  try {
    await navigator.clipboard.write([new ClipboardItem({ 'image/gif': state.resultBlob })]);
    showToast('GIF copied to the clipboard.');
  } catch {
    showToast('This browser cannot copy animated GIF files. Use Share or Download instead.');
  }
}

const exportPresets = {
  messages: { size: '480', tempo: '70', quality: '18', targetSize: '8388608' },
  discord: { size: '320', tempo: '70', quality: '12', targetSize: '8388608' },
  slack: { size: '480', tempo: '110', quality: '18', targetSize: '5242880' },
  quality: { size: '720', tempo: '70', quality: '24', targetSize: '15728640' },
};

function applyPreset() {
  const preset = exportPresets[elements.preset.value];
  if (!preset) return;
  elements.size.value = preset.size;
  elements.tempo.value = preset.tempo;
  elements.quality.value = preset.quality;
  elements.targetSize.value = preset.targetSize;
  editorChanged();
}

function editorSnapshot() {
  const snapshot = { prompt: elements.prompt.value };
  for (const key of editorControlKeys) {
    const control = elements[key];
    snapshot[key] = control.type === 'checkbox' ? control.checked : control.value;
  }
  return snapshot;
}

let historyTimer;
function recordEditorState() {
  if (state.applyingHistory) return;
  const snapshot = editorSnapshot();
  if (JSON.stringify(state.history[state.historyIndex]) === JSON.stringify(snapshot)) return;
  state.history = state.history.slice(0, state.historyIndex + 1);
  state.history.push(snapshot);
  if (state.history.length > 50) state.history.shift();
  state.historyIndex = state.history.length - 1;
  updateHistoryButtons();
}

function editorChanged() {
  if (state.applyingHistory || state.mode !== 'edit') return;
  updateEditorVisuals();
  if (state.resultBlob) clearResult();
  clearTimeout(historyTimer);
  historyTimer = setTimeout(recordEditorState, 180);
}

function applyEditorSnapshot(snapshot) {
  if (!snapshot) return;
  state.applyingHistory = true;
  elements.prompt.value = snapshot.prompt || '';
  for (const key of editorControlKeys) {
    const control = elements[key];
    if (!(key in snapshot)) continue;
    if (control.type === 'checkbox') control.checked = Boolean(snapshot[key]);
    else control.value = snapshot[key];
  }
  state.applyingHistory = false;
  updateEditorVisuals();
  if (state.resultBlob) clearResult();
  updateHistoryButtons();
}

function undoEditor() {
  if (state.historyIndex <= 0) return;
  state.historyIndex -= 1;
  applyEditorSnapshot(state.history[state.historyIndex]);
}

function redoEditor() {
  if (state.historyIndex >= state.history.length - 1) return;
  state.historyIndex += 1;
  applyEditorSnapshot(state.history[state.historyIndex]);
}

function updateHistoryButtons() {
  elements.undo.disabled = state.historyIndex <= 0;
  elements.redo.disabled = state.historyIndex < 0 || state.historyIndex >= state.history.length - 1;
}

function updateEditorVisuals() {
  const cropX = Number(elements.cropX.value);
  const cropY = Number(elements.cropY.value);
  const zoom = Number(elements.zoom.value);
  elements.cropXOutput.value = cropLabel(cropX, 'Left', 'Right');
  elements.cropYOutput.value = cropLabel(cropY, 'Top', 'Bottom');
  elements.zoomOutput.value = `${zoom.toFixed(2)}×`;
  const transform = `scale(${zoom}) translate(${-cropX * 8}%, ${-cropY * 8}%)`;
  if (!state.resultBlob) {
    elements.preview.style.transform = transform;
    elements.videoPreview.style.transform = transform;
  }
  const position = elements.captionPosition.value;
  elements.captionGuide.dataset.position = position;
  elements.captionGuide.textContent = elements.prompt.value.trim().toUpperCase() || 'CAPTION';
  const positions = { top: 0, middle: 1, bottom: 2 };
  elements.captionGuide.setAttribute('aria-valuenow', String(positions[position]));
  elements.captionGuide.setAttribute('aria-valuetext', position);
}

function cropLabel(value, negative, positive) {
  if (Math.abs(value) < 0.05) return 'Center';
  return `${value < 0 ? negative : positive} ${Math.round(Math.abs(value) * 100)}%`;
}

function clamp(value, minimum, maximum) {
  return Math.min(maximum, Math.max(minimum, value));
}

function startCropDrag(event) {
  if (state.mode !== 'edit' || !state.uploadFile || state.resultBlob || event.target === elements.captionGuide) return;
  if (state.uploadIsVideo && !event.target.classList.contains('crop-guide')) return;
  state.drag = { kind: 'crop', x: event.clientX, y: event.clientY, cropX: Number(elements.cropX.value), cropY: Number(elements.cropY.value) };
  elements.previewShell.classList.add('dragging');
  elements.previewShell.setPointerCapture(event.pointerId);
  event.preventDefault();
}

function moveCropDrag(event) {
  if (state.drag?.kind !== 'crop') return;
  const bounds = elements.previewShell.getBoundingClientRect();
  elements.cropX.value = String(clamp(state.drag.cropX + (event.clientX - state.drag.x) * 2 / bounds.width, -1, 1));
  elements.cropY.value = String(clamp(state.drag.cropY + (event.clientY - state.drag.y) * 2 / bounds.height, -1, 1));
  updateEditorVisuals();
}

function finishCropDrag() {
  if (state.drag?.kind !== 'crop') return;
  state.drag = null;
  elements.previewShell.classList.remove('dragging');
  recordEditorState();
}

function startCaptionDrag(event) {
  if (state.resultBlob) return;
  state.drag = { kind: 'caption' };
  elements.captionGuide.setPointerCapture(event.pointerId);
  event.stopPropagation();
  event.preventDefault();
}

function moveCaptionDrag(event) {
  if (state.drag?.kind !== 'caption') return;
  const bounds = elements.previewShell.getBoundingClientRect();
  const progress = clamp((event.clientY - bounds.top) / bounds.height, 0, 1);
  elements.captionPosition.value = progress < 0.34 ? 'top' : progress < 0.67 ? 'middle' : 'bottom';
  updateEditorVisuals();
}

function finishCaptionDrag() {
  if (state.drag?.kind !== 'caption') return;
  state.drag = null;
  recordEditorState();
}

function keyboardCrop(event) {
  if (state.mode !== 'edit' || !state.uploadFile || state.resultBlob || !event.key.startsWith('Arrow')) return;
  const step = event.shiftKey ? 0.2 : 0.05;
  if (event.key === 'ArrowLeft') elements.cropX.value = String(clamp(Number(elements.cropX.value) - step, -1, 1));
  if (event.key === 'ArrowRight') elements.cropX.value = String(clamp(Number(elements.cropX.value) + step, -1, 1));
  if (event.key === 'ArrowUp') elements.cropY.value = String(clamp(Number(elements.cropY.value) - step, -1, 1));
  if (event.key === 'ArrowDown') elements.cropY.value = String(clamp(Number(elements.cropY.value) + step, -1, 1));
  event.preventDefault();
  updateEditorVisuals();
  recordEditorState();
}

function keyboardCaption(event) {
  if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown') return;
  const positions = ['top', 'middle', 'bottom'];
  const direction = event.key === 'ArrowUp' ? -1 : 1;
  const index = clamp(positions.indexOf(elements.captionPosition.value) + direction, 0, 2);
  elements.captionPosition.value = positions[index];
  event.preventDefault();
  updateEditorVisuals();
  recordEditorState();
}

const draftDatabase = new Promise((resolve, reject) => {
  if (!('indexedDB' in window)) {
    reject(new Error('Local drafts are unavailable in this browser.'));
    return;
  }
  const request = indexedDB.open('gogif-editor', 1);
  request.onupgradeneeded = () => request.result.createObjectStore('drafts', { keyPath: 'id' });
  request.onsuccess = () => resolve(request.result);
  request.onerror = () => reject(request.error);
});

async function draftOperation(mode, operation) {
  const database = await draftDatabase;
  return new Promise((resolve, reject) => {
    const transaction = database.transaction('drafts', mode);
    const result = operation(transaction.objectStore('drafts'));
    result.onsuccess = () => resolve(result.result);
    result.onerror = () => reject(result.error);
  });
}

async function refreshDrafts() {
  try {
    const drafts = await draftOperation('readonly', (store) => store.getAll());
    drafts.sort((a, b) => b.updatedAt - a.updatedAt);
    elements.drafts.replaceChildren(new Option(drafts.length ? 'Choose a saved draft' : 'No saved drafts', ''));
    for (const draft of drafts) {
      const date = new Date(draft.updatedAt).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
      elements.drafts.append(new Option(`${draft.filename} · ${date}`, draft.id));
    }
    if (drafts.some((draft) => draft.id === state.currentDraftID)) elements.drafts.value = state.currentDraftID;
    updateDraftButtons();
  } catch {
    elements.saveDraft.disabled = true;
    elements.drafts.disabled = true;
  }
}

async function saveDraft() {
  if (!state.uploadFile) {
    showToast('Choose media before saving a draft.');
    return;
  }
  const id = state.currentDraftID || (crypto.randomUUID?.() || `draft-${Date.now()}`);
  try {
    await draftOperation('readwrite', (store) => store.put({
      id, updatedAt: Date.now(), filename: state.uploadFile.name, type: state.uploadFile.type,
      media: state.uploadFile, settings: editorSnapshot(),
    }));
    state.currentDraftID = id;
    await refreshDrafts();
    showToast('Draft saved only in this browser.');
  } catch {
    showToast('Could not save this local draft.');
  }
}

async function loadDraft() {
  if (!elements.drafts.value) return;
  try {
    const draft = await draftOperation('readonly', (store) => store.get(elements.drafts.value));
    if (!draft) throw new Error('missing draft');
    const file = draft.media instanceof File ? draft.media : new File([draft.media], draft.filename, { type: draft.type });
    loadUploadFile(file, true);
    applyEditorSnapshot(draft.settings);
    state.currentDraftID = draft.id;
    state.history = [draft.settings];
    state.historyIndex = 0;
    updateHistoryButtons();
    showToast('Local draft loaded.');
  } catch {
    showToast('Could not load this local draft.');
  }
}

async function deleteDraft() {
  if (!elements.drafts.value) return;
  try {
    const id = elements.drafts.value;
    await draftOperation('readwrite', (store) => store.delete(id));
    if (state.currentDraftID === id) state.currentDraftID = '';
    await refreshDrafts();
    showToast('Local draft deleted.');
  } catch {
    showToast('Could not delete this local draft.');
  }
}

function updateDraftButtons() {
  const selected = Boolean(elements.drafts.value);
  elements.loadDraft.disabled = !selected;
  elements.deleteDraft.disabled = !selected;
}

function updateSearchScope() {
  if (!elements.prompt.value.trim()) clearSearchResults();
}

function clearSearchResults() {
  clearTimeout(searchTimer);
  state.searchRequestID += 1;
  elements.searchResults.replaceChildren();
  elements.searchMessage.hidden = true;
  elements.searchMessage.textContent = '';
  elements.searchTitle.textContent = elements.searchScope.value === 'gifs' ? 'Find a GIF' : 'Find source media';
}

function queueSearch() {
  clearTimeout(searchTimer);
  state.searchRequestID += 1;
  if (state.mode !== 'search') return;
  const query = elements.prompt.value.trim();
  if (!query) {
    clearSearchResults();
    return;
  }
  searchTimer = setTimeout(() => search(query), 350);
}

async function search(query) {
  clearTimeout(searchTimer);
  query = query.trim();
  if (!query) {
    clearSearchResults();
    return;
  }
  const requestID = ++state.searchRequestID;
  const searchScope = elements.searchScope.value;
  elements.searchResults.replaceChildren();
  elements.searchMessage.hidden = false;
  elements.searchMessage.textContent = searchScope === 'gifs' ? 'Searching actual GIFs…' : 'Searching source clips and images…';
  elements.searchTitle.textContent = `“${query}”`;

  const apiKey = state.config?.giphy_api_key;
  const searches = searchScope === 'gifs'
    ? apiKey ? [searchGiphy(query, apiKey)] : [searchGifCities(query)]
    : [searchWikimedia(query), searchPrelinger(query), searchNASA(query)];
  const settled = await Promise.allSettled(searches);
  if (requestID !== state.searchRequestID || state.mode !== 'search' || elements.prompt.value.trim() !== query || elements.searchScope.value !== searchScope) return;

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
  elements.searchMessage.hidden = false;
  if (resultCount) {
    elements.searchMessage.hidden = true;
    elements.searchMessage.textContent = '';
  } else {
    elements.searchMessage.textContent = failures[0] || 'No matches yet. Try a broader feeling, action, or subject.';
  }
  scrollToElement(elements.searchPanel);
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
			provider: item.provider,
			externalID: item.external_id,
			kind: item.kind,
			href: item.source_url,
			preview: item.preview_url,
			mediaURL: item.original_url || item.preview_url,
			title: item.title || query,
			note: [item.author, item.license_name].filter(Boolean).join(' · '),
			allowedHandling: item.allowed_handling,
			transformPolicy: item.transform_policy,
			derivatives: item.derivatives,
		})),
	};
}

async function searchGifCities(query) {
	const url = new URL('/api/v1/providers/gifcities/search', window.location.origin);
	url.search = new URLSearchParams({ q: query, limit: '18', locale: 'en' });
	const response = await fetch(url);
	const payload = await response.json().catch(() => ({}));
	if (!response.ok) throw new Error(payload.error?.message || 'GifCities search failed.');
	return {
		label: 'GifCities',
		credit: 'INTERNET ARCHIVE · ARCHIVED GEOCITIES',
		items: payload.results.map((item) => ({
			provider: item.provider,
			externalID: item.external_id,
			kind: item.kind,
			href: item.source_url,
			preview: item.preview_url,
			mediaURL: item.original_url || item.preview_url,
			title: item.title || query,
			note: 'Archived source · rights not supplied',
			allowedHandling: item.allowed_handling,
			transformPolicy: item.transform_policy,
			derivatives: item.derivatives,
		})),
	};
}

async function searchPrelinger(query) {
	const url = new URL('/api/v1/providers/prelinger/search', window.location.origin);
	url.search = new URLSearchParams({ q: query, limit: '18', locale: 'en' });
	const response = await fetch(url);
	const payload = await response.json().catch(() => ({}));
	if (!response.ok) throw new Error(payload.error?.message || 'Prelinger Archive search failed.');
	return {
		label: 'Prelinger Archive',
		credit: 'INTERNET ARCHIVE · ITEM-SPECIFIC LICENSES',
		items: payload.results.map((item) => ({
			provider: item.provider,
			externalID: item.external_id,
			kind: item.kind,
			href: item.source_url,
			preview: item.preview_url,
			mediaURL: item.original_url || item.preview_url,
			title: item.title || query,
			query,
			note: [item.author, item.license_name || 'Check item rights'].filter(Boolean).join(' · '),
			allowedHandling: item.allowed_handling,
			transformPolicy: item.transform_policy,
			derivatives: item.derivatives,
		})),
	};
}

async function searchNASA(query) {
	const url = new URL('/api/v1/providers/nasa/search', window.location.origin);
	url.search = new URLSearchParams({ q: query, limit: '18', locale: 'en' });
	const response = await fetch(url);
	const payload = await response.json().catch(() => ({}));
	if (!response.ok) throw new Error(payload.error?.message || 'NASA media search failed.');
	return {
		label: 'NASA Image and Video Library',
		credit: 'NASA · SOURCE ACKNOWLEDGMENT REQUIRED',
		items: payload.results.map((item) => ({
			provider: item.provider,
			externalID: item.external_id,
			kind: item.kind,
			href: item.source_url,
			preview: item.preview_url,
			mediaURL: item.original_url || item.preview_url,
			title: item.title || query,
			note: [item.author, 'Review third-party, logo, and likeness restrictions'].filter(Boolean).join(' · '),
			allowedHandling: item.allowed_handling,
			transformPolicy: item.transform_policy,
			derivatives: item.derivatives,
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
			const gifURL = preview.url || item.images.original.url;
			return {
				provider: 'giphy',
				kind: 'gif',
				href: item.url || item.images.original.url,
				preview: gifURL,
				mediaURL: item.images.original.url || gifURL,
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
	const card = document.createElement('article');
	card.className = 'search-card';
	const media = document.createElement('div');
	media.className = 'search-card-media';
	const image = document.createElement('img');
	image.src = item.preview;
	image.alt = item.title;
	image.loading = 'lazy';
	if (item.kind === 'gif') image.setAttribute('aria-label', `${item.title}. Animated GIF; touch and hold to copy.`);
	media.append(image);
	card.append(media);
	const details = document.createElement('div');
	details.className = 'search-card-details';
	const links = document.createElement('div');
	links.className = 'search-card-links';
	if (item.kind === 'gif' && (item.mediaURL || item.preview)) {
		const openGIF = document.createElement('a');
		openGIF.href = item.mediaURL || item.preview;
		openGIF.target = '_blank';
		openGIF.rel = 'noreferrer';
		openGIF.textContent = 'Open GIF';
		links.append(openGIF);
	}
	if (item.href) {
		const source = document.createElement('a');
		source.href = item.href;
		source.target = '_blank';
		source.rel = 'noreferrer';
		source.textContent = 'Source';
		links.append(source);
	}
	if (links.childElementCount) details.append(links);
	card.append(details);
	if ((item.kind === 'video' || item.kind === 'clip') && item.allowedHandling?.includes('display')) {
		const preview = document.createElement('button');
		preview.className = 'preview-button';
		preview.type = 'button';
		preview.textContent = 'Load video preview';
		preview.addEventListener('click', () => toggleVideoPreview(item, card, media, image, preview));
		card.append(preview);
	}
	const canRemix = state.config?.image_generator?.supports_references
		&& item.transformPolicy === 'allowed'
		&& item.derivatives === 'allowed'
		&& item.allowedHandling?.includes('temporary-transform')
		&& (item.kind === 'image' || item.kind === 'gif');
	if (canRemix) {
		const remix = document.createElement('button');
		remix.className = 'remix-button';
		remix.type = 'button';
		remix.textContent = 'Use as reference';
		remix.addEventListener('click', () => selectReference(item));
		card.append(remix);
	}
	return card;
}

async function toggleVideoPreview(item, card, media, image, button) {
	const existing = card.querySelector('video');
	if (existing) {
		existing.hidden = !existing.hidden;
		image.hidden = !existing.hidden;
		if (existing.hidden) existing.pause();
		button.textContent = existing.hidden ? 'Show video preview' : 'Hide video preview';
		return;
	}
	button.disabled = true;
	button.textContent = 'Loading preview…';
	try {
		const itemPath = `/api/v1/providers/${encodeURIComponent(item.provider)}/items/${encodeURIComponent(item.externalID)}`;
		const url = new URL(item.query ? `${itemPath}/quote` : itemPath, window.location.origin);
		url.searchParams.set('locale', 'en');
		if (item.query) url.searchParams.set('q', item.query);
		const response = await fetch(url);
		const payload = await response.json().catch(() => ({}));
		if (!response.ok) throw new Error(payload.error?.message || 'Could not load this video preview.');
		const rendition = payload.renditions?.find((candidate) => candidate.content_type === 'video/mp4') || payload.renditions?.[0];
		if (!rendition?.url) throw new Error('This item has no browser-compatible video rendition.');
		const video = document.createElement('video');
		video.controls = true;
		video.playsInline = true;
		video.preload = 'metadata';
		video.poster = item.preview;
		video.src = rendition.url;
		if (payload.quote_match) {
			const match = payload.quote_match;
			const startMS = match.start_ms || 0;
			const start = Math.max(0, startMS / 1000 - 0.35);
			const end = match.end_ms / 1000 + 0.5;
			let stopAtMatchEnd = true;
			video.addEventListener('loadedmetadata', () => { video.currentTime = start; }, { once: true });
			video.addEventListener('timeupdate', () => {
				if (stopAtMatchEnd && video.currentTime >= end) {
					video.pause();
					stopAtMatchEnd = false;
				}
			});
			const quote = document.createElement('span');
			quote.className = 'quote-match';
			quote.textContent = `${match.exact ? 'Exact quote' : 'Closest quote'} · ${formatTimecode(startMS)} · “${match.text}”`;
			card.insertBefore(quote, button);
		}
		media.append(video);
		image.hidden = true;
		button.textContent = 'Hide video preview';
	} catch (error) {
		button.textContent = 'Try video preview again';
		showToast(error.message);
	} finally {
		button.disabled = false;
	}
}

function formatTimecode(milliseconds) {
	const totalSeconds = Math.max(0, Math.floor(milliseconds / 1000));
	const hours = Math.floor(totalSeconds / 3600);
	const minutes = Math.floor(totalSeconds / 60) % 60;
	const seconds = String(totalSeconds % 60).padStart(2, '0');
	return hours ? `${hours}:${String(minutes).padStart(2, '0')}:${seconds}` : `${minutes}:${seconds}`;
}

function selectReference(item) {
	state.reference = item;
	elements.referenceLabel.textContent = item.title;
	elements.referenceChip.hidden = false;
	setMode('create');
	if (!elements.prompt.value.trim()) elements.prompt.value = `Remix ${item.title}`;
	elements.prompt.focus();
	showToast('Reference selected. The source file will only exist temporarily during generation.');
}

function clearReference() {
	state.reference = null;
	elements.referenceChip.hidden = true;
	elements.referenceLabel.textContent = '';
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
    if (state.mode === 'search') queueSearch();
  });
}
elements.form.addEventListener('submit', submitPrompt);
elements.searchScope.addEventListener('change', () => {
  updateSearchScope();
  queueSearch();
});
elements.referenceClear.addEventListener('click', clearReference);
elements.uploadMedia.addEventListener('change', selectUpload);
elements.copy.addEventListener('click', copyResult);
elements.share.addEventListener('click', shareResult);
elements.preset.addEventListener('change', applyPreset);
for (const control of [elements.size, elements.tempo, elements.quality, elements.targetSize]) {
  control.addEventListener('change', () => {
    elements.preset.value = 'custom';
    editorChanged();
  });
}
for (const key of editorControlKeys.filter((key) => !['size', 'tempo', 'quality', 'targetSize'].includes(key))) {
  elements[key].addEventListener('input', editorChanged);
  elements[key].addEventListener('change', editorChanged);
}
elements.undo.addEventListener('click', undoEditor);
elements.redo.addEventListener('click', redoEditor);
elements.saveDraft.addEventListener('click', saveDraft);
elements.drafts.addEventListener('change', updateDraftButtons);
elements.loadDraft.addEventListener('click', loadDraft);
elements.deleteDraft.addEventListener('click', deleteDraft);
elements.previewShell.addEventListener('pointerdown', startCropDrag);
elements.previewShell.addEventListener('pointermove', moveCropDrag);
elements.previewShell.addEventListener('pointerup', finishCropDrag);
elements.previewShell.addEventListener('pointercancel', finishCropDrag);
elements.previewShell.addEventListener('keydown', keyboardCrop);
elements.captionGuide.addEventListener('pointerdown', startCaptionDrag);
elements.captionGuide.addEventListener('pointermove', moveCaptionDrag);
elements.captionGuide.addEventListener('pointerup', finishCaptionDrag);
elements.captionGuide.addEventListener('pointercancel', finishCaptionDrag);
elements.captionGuide.addEventListener('keydown', keyboardCaption);
elements.trimStart.addEventListener('change', () => {
  if (state.uploadIsVideo && Number.isFinite(Number(elements.trimStart.value))) {
    elements.videoPreview.currentTime = Number(elements.trimStart.value);
  }
});
elements.reroll.addEventListener('click', () => {
  if (state.mode === 'edit') exportUpload(elements.prompt.value.trim());
  else generate(elements.prompt.value.trim(), true);
});
elements.prompt.addEventListener('input', () => {
  elements.prompt.style.height = 'auto';
  elements.prompt.style.height = `${Math.min(elements.prompt.scrollHeight, 130)}px`;
  editorChanged();
  if (state.mode === 'search') queueSearch();
});
document.addEventListener('keydown', (event) => {
  if (state.mode !== 'edit' || !(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== 'z') return;
  event.preventDefault();
  if (event.shiftKey) redoEditor();
  else undoEditor();
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
refreshDrafts();
updateEditorVisuals();
