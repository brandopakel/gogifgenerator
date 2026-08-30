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
	modelPreview: document.querySelector('#model-preview'),
  editorOverlay: document.querySelector('#editor-overlay'),
  captionGuide: document.querySelector('#caption-guide'),
  previewEmpty: document.querySelector('#preview-empty'),
  working: document.querySelector('#working'),
  resultTitle: document.querySelector('#result-title'),
  download: document.querySelector('#download-button'),
  reroll: document.querySelector('#reroll-button'),
  copy: document.querySelector('#copy-button'),
  share: document.querySelector('#share-button'),
  generationModeField: document.querySelector('#generation-mode-field'),
  generationMode: document.querySelector('#generation-mode-control'),
	generationControls: document.querySelector('#generation-controls'),
  presetField: document.querySelector('#preset-field'),
  preset: document.querySelector('#preset-control'),
  size: document.querySelector('#size-control'),
  tempo: document.querySelector('#tempo-control'),
  quality: document.querySelector('#quality-control'),
  targetSizeField: document.querySelector('#target-size-field'),
  targetSize: document.querySelector('#target-size-control'),
  searchTitle: document.querySelector('#search-title'),
  searchMessage: document.querySelector('#search-message'),
  searchResults: document.querySelector('#search-results'),
  searchSentinel: document.querySelector('#search-sentinel'),
  searchOptions: document.querySelector('#search-options'),
  searchScope: document.querySelector('#search-scope'),
	modelOptions: document.querySelector('#model-options'),
	modelRecipe: document.querySelector('#model-recipe'),
  install: document.querySelector('#install-button'),
  referenceChip: document.querySelector('#reference-chip'),
  referenceLabel: document.querySelector('#reference-label'),
  referenceClear: document.querySelector('#reference-clear'),
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
  mode: 'create', config: null, objectURL: '', resultURL: '', uploadPreviewURL: '', resultBlob: null, resultKind: '',
  uploadFile: null, seed: 0, installPrompt: null, reference: null, uploadIsVideo: false,
  history: [], historyIndex: -1, applyingHistory: false, currentDraftID: '', drag: null,
  searchRequestID: 0, searchSession: null,
};

const prefersReducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
const editorControlKeys = ['captionPosition', 'motion', 'cropX', 'cropY', 'zoom', 'trimStart', 'trimEnd', 'loop', 'size', 'tempo', 'quality', 'targetSize'];
let searchTimer;
let searchContinuationTimer;

async function loadConfig() {
  try {
    const response = await fetch('/api/v1/config');
    state.config = await response.json();
    if (state.config.video_editor?.enabled) {
      elements.videoCapability.hidden = true;
    } else {
      elements.videoCapability.textContent = 'Video requires FFmpeg.';
      elements.videoCapability.hidden = false;
    }
    const semanticOption = elements.generationMode.querySelector('option[value="semantic"]');
    const semanticGenerator = state.config.image_generator?.semantic;
    semanticOption.textContent = semanticGenerator
      ? `Realistic AI · ${state.config.image_generator.label}`
      : 'Realistic AI · setup required';
	const recipes = state.config.model_generator?.recipes || [];
	elements.modelRecipe.replaceChildren();
	if (recipes.length) {
	  for (const recipe of recipes) elements.modelRecipe.append(new Option(recipe.label, recipe.id));
	} else {
	  elements.modelRecipe.append(new Option('ComfyUI 3D · setup required', 'tripo-3.1'));
	}
    updateSearchScope();
    if (state.mode === 'search') queueSearch();
  } catch {
    showToast('Could not read app configuration.');
  }
}

function setMode(mode) {
	const changingMediaType = (mode === 'model') !== (state.mode === 'model');
  state.mode = mode;
	if (changingMediaType && state.resultBlob) clearResult(mode === 'edit');
  for (const button of elements.modes) {
    const active = button.dataset.mode === mode;
    button.classList.toggle('active', active);
    button.setAttribute('aria-selected', String(active));
  }
  const searching = mode === 'search';
  const editing = mode === 'edit';
	const modeling = mode === 'model';
  elements.createPanel.hidden = searching;
  elements.searchPanel.hidden = !searching;
  elements.searchOptions.hidden = !searching;
	elements.modelOptions.hidden = !modeling;
  elements.uploadEditor.hidden = !editing;
  elements.editControls.hidden = !editing;
	elements.referenceChip.hidden = editing || modeling || !state.reference;
  elements.prompt.required = !editing;
	elements.submitLabel.textContent = editing ? 'Export GIF' : searching ? 'Find it' : modeling ? 'Build 3D' : 'Make it';
	elements.submit.setAttribute('aria-label', editing ? 'Export edited GIF' : searching ? 'Search GIFs' : modeling ? 'Create 3D model' : 'Create GIF');
	elements.prompt.placeholder = editing ? 'Add a caption (optional)…' : searching ? 'Search GIFs or clips…' : modeling ? 'Describe a 3D model…' : 'Describe a GIF…';
  elements.reroll.textContent = editing ? 'Re-export' : 'Reroll';
  elements.presetField.hidden = !editing;
  elements.targetSizeField.hidden = !editing;
	elements.generationModeField.hidden = editing || modeling;
	elements.generationControls.hidden = modeling;
	elements.prompt.maxLength = editing ? 42 : modeling ? 1024 : 500;
  elements.previewShell.classList.toggle('editing', editing && Boolean(state.uploadFile) && !state.resultBlob);
  elements.editorOverlay.hidden = !(editing && state.uploadFile && !state.resultBlob);
	if (modeling && !state.resultBlob) {
	  elements.preview.hidden = true;
	  elements.videoPreview.hidden = true;
	  elements.modelPreview.hidden = true;
	  elements.previewEmpty.hidden = false;
	} else if (editing && state.uploadFile && !state.resultBlob) {
	  elements.preview.src = state.uploadIsVideo ? '' : state.uploadPreviewURL;
	  elements.preview.hidden = state.uploadIsVideo;
	  elements.videoPreview.hidden = !state.uploadIsVideo;
	  elements.previewEmpty.hidden = true;
	}
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
	} else if (prompt && state.mode === 'model') {
	  await generateModel(prompt);
  } else if (prompt && state.mode === 'create') {
    await generate(prompt);
  } else if (prompt) {
    await search(prompt);
  }
}

async function generateModel(prompt, reroll = false) {
	setWorking(true, 'Building the 3D asset…');
	if (reroll) state.seed = Date.now();
	try {
	  const response = await fetch('/api/v1/models/generate', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ prompt, recipe: elements.modelRecipe.value, seed: state.seed }),
	  });
	  if (!response.ok) {
		const problem = await response.json().catch(() => ({}));
		throw new Error(problem.error?.message || `3D generation failed (${response.status})`);
	  }
	  const resultURL = response.headers.get('Location');
	  const blob = await response.blob();
	  presentModelResult(blob, resultURL);
	  elements.resultTitle.textContent = prompt.length > 48 ? `${prompt.slice(0, 48)}…` : prompt;
	  elements.reroll.disabled = false;
	  scrollToElement(elements.createPanel);
	} catch (error) {
	  showToast(error.message);
	} finally {
	  setWorking(false);
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
      generation_mode: elements.generationMode.value,
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
    const resultURL = response.headers.get('Location');
    const blob = await response.blob();
    presentResult(blob, resultURL);
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
	elements.modelPreview.hidden = true;
	elements.modelPreview.removeAttribute('src');
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
    const resultURL = response.headers.get('Location');
    const blob = await response.blob();
    presentResult(blob, resultURL);
    elements.resultTitle.textContent = caption || state.uploadFile.name;
    elements.reroll.disabled = false;
    scrollToElement(elements.createPanel);
  } catch (error) {
    showToast(error.message);
  } finally {
    setWorking(false);
  }
}

function presentResult(blob, resultURL = '') {
  if (state.objectURL) URL.revokeObjectURL(state.objectURL);
  state.resultBlob = blob;
	state.resultKind = 'gif';
  state.resultURL = resultURL ? new URL(resultURL, window.location.origin).href : '';
  state.objectURL = URL.createObjectURL(blob);
  elements.preview.src = state.objectURL;
  elements.preview.hidden = false;
  elements.preview.style.transform = '';
  elements.videoPreview.hidden = true;
  elements.videoPreview.pause();
	elements.modelPreview.hidden = true;
	elements.modelPreview.removeAttribute('src');
  elements.editorOverlay.hidden = true;
  elements.previewShell.classList.remove('editing', 'dragging');
  elements.previewShell.classList.add('has-result');
  elements.preview.setAttribute('aria-label', 'Generated GIF. On iPhone, touch and hold to copy or save it.');
  elements.previewEmpty.hidden = true;
  elements.download.href = state.objectURL;
	elements.download.download = 'gogif.gif';
	elements.download.textContent = 'Download GIF';
  elements.download.classList.remove('disabled');
  elements.download.setAttribute('aria-disabled', 'false');
  elements.share.disabled = false;
  elements.copy.disabled = false;
}

function presentModelResult(blob, resultURL = '') {
	if (state.objectURL) URL.revokeObjectURL(state.objectURL);
	state.resultBlob = blob;
	state.resultKind = 'model';
	state.resultURL = resultURL ? new URL(resultURL, window.location.origin).href : '';
	state.objectURL = URL.createObjectURL(blob);
	elements.preview.hidden = true;
	elements.videoPreview.hidden = true;
	elements.videoPreview.pause();
	elements.modelPreview.src = state.objectURL;
	elements.modelPreview.hidden = false;
	elements.editorOverlay.hidden = true;
	elements.previewShell.classList.remove('editing', 'dragging');
	elements.previewShell.classList.add('has-result');
	elements.previewEmpty.hidden = true;
	elements.download.href = state.objectURL;
	elements.download.download = 'gogif-model.glb';
	elements.download.textContent = 'Save .GLB';
	elements.download.classList.remove('disabled');
	elements.download.setAttribute('aria-disabled', 'false');
	elements.share.disabled = false;
	elements.copy.disabled = false;
}

function clearResult(restoreUpload = true) {
  if (state.objectURL) URL.revokeObjectURL(state.objectURL);
  state.objectURL = '';
  state.resultURL = '';
  state.resultBlob = null;
	state.resultKind = '';
	elements.modelPreview.hidden = true;
	elements.modelPreview.removeAttribute('src');
  elements.previewShell.classList.remove('has-result');
  elements.preview.setAttribute('aria-label', 'GIF preview');
  elements.download.removeAttribute('href');
	elements.download.download = 'gogif.gif';
	elements.download.textContent = 'Download GIF';
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
	} else {
	  elements.preview.hidden = true;
	  elements.videoPreview.hidden = true;
	  elements.previewEmpty.hidden = false;
	  elements.resultTitle.textContent = 'Ready when you are';
  }
}

async function shareResult() {
  if (!state.resultBlob) return;
	const isModel = state.resultKind === 'model';
	const filename = isModel ? 'gogif-model.glb' : 'gogif.gif';
	const contentType = isModel ? 'model/gltf-binary' : 'image/gif';
	const file = new File([state.resultBlob], filename, { type: contentType });
  const shareData = { files: [file], title: 'GoGIF', text: elements.resultTitle.textContent };
  if (navigator.share && (!navigator.canShare || navigator.canShare(shareData))) {
    try {
      await navigator.share(shareData);
      return;
    } catch (error) {
      if (error.name === 'AbortError') return;
    }
  }
	if (await copyResultLink()) showToast(`This browser cannot share the ${isModel ? 'GLB file' : 'GIF'}, so its link was copied.`);
	else showToast(`File sharing is unavailable here. Save the ${isModel ? 'GLB' : 'GIF'} instead.`);
}

async function copyResult() {
  if (!state.resultBlob) return;
	const isModel = state.resultKind === 'model';
	const contentType = isModel ? 'model/gltf-binary' : 'image/gif';
  if (navigator.clipboard?.write && typeof ClipboardItem !== 'undefined') {
    try {
		if (ClipboardItem.supports && !ClipboardItem.supports(contentType)) throw new Error('unsupported clipboard type');
		await navigator.clipboard.write([new ClipboardItem({ [contentType]: state.resultBlob })]);
		showToast(`${isModel ? '3D model' : 'GIF'} copied to the clipboard.`);
      return;
    } catch {
      // Most desktop browsers reject animated GIF clipboard items. Fall back to its hosted URL.
    }
  }
	if (await copyResultLink()) showToast(`This browser cannot copy ${isModel ? 'GLB' : 'animated GIF'} data, so its link was copied.`);
	else showToast(`This browser cannot copy the ${isModel ? '3D model' : 'GIF'}. Save it instead.`);
}

async function copyResultLink() {
  if (!state.resultURL || !navigator.clipboard?.writeText) return false;
  try {
    await navigator.clipboard.writeText(state.resultURL);
    return true;
  } catch {
    return false;
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
  clearTimeout(searchContinuationTimer);
  searchContinuationTimer = 0;
  state.searchRequestID += 1;
  state.searchSession = null;
  elements.searchResults.replaceChildren();
  elements.searchSentinel.hidden = true;
  elements.searchSentinel.textContent = '';
  elements.searchMessage.hidden = true;
  elements.searchMessage.textContent = '';
  const emptyTitles = { gifs: 'Find a GIF', stickers: 'Find a sticker', source: 'Find source media' };
  elements.searchTitle.textContent = emptyTitles[elements.searchScope.value] || 'Find media';
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
  clearTimeout(searchContinuationTimer);
  searchContinuationTimer = 0;
  query = query.trim();
  if (!query) {
    clearSearchResults();
    return;
  }
  const requestID = ++state.searchRequestID;
  const searchScope = elements.searchScope.value;
  const apiKey = state.config?.giphy_api_key;
  const loaders = searchScope === 'gifs'
    ? apiKey
      ? [{ id: 'giphy', load: (cursor) => searchGiphy(query, apiKey, cursor, 'gifs') }]
      : [{ id: 'gifcities', load: (cursor) => searchGifCities(query, cursor) }]
    : searchScope === 'stickers'
      ? apiKey
        ? [{ id: 'giphy-stickers', load: (cursor) => searchGiphy(query, apiKey, cursor, 'stickers') }]
        : []
      : [
        { id: 'wikimedia', load: (cursor) => searchWikimedia(query, cursor) },
        { id: 'prelinger', load: (cursor) => searchPrelinger(query, cursor) },
        { id: 'nasa', load: (cursor) => searchNASA(query, cursor) },
      ];
  const session = {
    requestID, query, searchScope, loading: false, resultCount: 0,
    providers: loaders.map((loader) => ({ ...loader, cursor: '', done: false, seen: new Set() })),
  };
  state.searchSession = session;
  elements.searchResults.replaceChildren();
  elements.searchMessage.hidden = false;
  const searchingMessages = { gifs: 'Searching actual GIFs…', stickers: 'Searching stickers…', source: 'Searching source clips and images…' };
  elements.searchMessage.textContent = searchingMessages[searchScope] || 'Searching…';
  elements.searchTitle.textContent = `“${query}”`;
  elements.searchSentinel.hidden = true;
  elements.searchSentinel.textContent = '';

  if (!loaders.length) {
    elements.searchMessage.textContent = 'Sticker search needs a configured GIPHY key.';
    return;
  }

  await loadMoreSearchResults(session);
  if (searchSessionIsActive(session)) scrollToElement(elements.searchPanel);
}

function searchSessionIsActive(session) {
  return state.searchSession === session
    && session.requestID === state.searchRequestID
    && state.mode === 'search'
    && elements.prompt.value.trim() === session.query
    && elements.searchScope.value === session.searchScope;
}

async function loadMoreSearchResults(session = state.searchSession) {
  if (!session || session.loading || !searchSessionIsActive(session)) return;
  const activeProviders = session.providers.filter((provider) => !provider.done);
  if (!activeProviders.length) {
    updateSearchSentinel(session);
    return;
  }
  session.loading = true;
  elements.searchSentinel.hidden = false;
  elements.searchSentinel.textContent = session.resultCount ? 'Loading more…' : '';
  const settled = await Promise.allSettled(activeProviders.map((provider) => provider.load(provider.cursor)));
  if (!searchSessionIsActive(session)) return;

  const failures = [];
  for (const [index, outcome] of settled.entries()) {
    const provider = activeProviders[index];
    if (outcome.status === 'rejected') {
      failures.push(outcome.reason.message);
      provider.done = true;
      continue;
    }
    provider.cursor = outcome.value.cursor || '';
    provider.done = !provider.cursor;
    const freshItems = outcome.value.items.filter((item, itemIndex) => {
      const key = item.externalID || item.mediaURL || item.preview || `${provider.id}-${itemIndex}`;
      if (provider.seen.has(key)) return false;
      provider.seen.add(key);
      return true;
    });
    if (freshItems.length) {
      session.resultCount += freshItems.length;
      renderProvider({ ...outcome.value, items: freshItems });
    }
  }
  session.loading = false;
  elements.searchMessage.hidden = false;
  if (session.resultCount) {
    elements.searchMessage.hidden = true;
    elements.searchMessage.textContent = '';
  } else {
    elements.searchMessage.textContent = failures[0] || 'No matches yet. Try a broader feeling, action, or subject.';
  }
  updateSearchSentinel(session);
}

function updateSearchSentinel(session) {
  const hasMore = session.providers.some((provider) => !provider.done);
  elements.searchSentinel.hidden = session.resultCount === 0;
  elements.searchSentinel.textContent = hasMore ? '' : 'End of results';
  if (hasMore) scheduleSearchContinuation(session);
}

function scheduleSearchContinuation(session = state.searchSession) {
  if (searchContinuationTimer) return;
  searchContinuationTimer = setTimeout(() => {
    searchContinuationTimer = 0;
    if (!session || !searchSessionIsActive(session)) return;
    const bounds = elements.searchSentinel.getBoundingClientRect();
    if (!elements.searchSentinel.hidden && bounds.top <= window.innerHeight + 700) loadMoreSearchResults(session);
  }, 0);
}

async function searchWikimedia(query, cursor = '') {
	const url = new URL('/api/v1/providers/wikimedia/search', window.location.origin);
	url.search = new URLSearchParams({ q: query, limit: '24', locale: 'en' });
	if (cursor) url.searchParams.set('cursor', cursor);
	const response = await fetch(url);
	const payload = await response.json().catch(() => ({}));
	if (!response.ok) throw new Error(payload.error?.message || 'Wikimedia Commons search failed.');
	return {
		id: 'wikimedia',
		label: 'Wikimedia Commons',
		credit: 'OPEN MEDIA · CHECK EACH LICENSE',
		cursor: payload.cursor || '',
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

async function searchGifCities(query, cursor = '') {
	const url = new URL('/api/v1/providers/gifcities/search', window.location.origin);
	url.search = new URLSearchParams({ q: query, limit: '24', locale: 'en' });
	if (cursor) url.searchParams.set('cursor', cursor);
	const response = await fetch(url);
	const payload = await response.json().catch(() => ({}));
	if (!response.ok) throw new Error(payload.error?.message || 'GifCities search failed.');
	return {
		id: 'gifcities',
		label: 'GifCities',
		credit: 'INTERNET ARCHIVE · ARCHIVED GEOCITIES',
		cursor: payload.cursor || '',
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

async function searchPrelinger(query, cursor = '') {
	const url = new URL('/api/v1/providers/prelinger/search', window.location.origin);
	url.search = new URLSearchParams({ q: query, limit: '24', locale: 'en' });
	if (cursor) url.searchParams.set('cursor', cursor);
	const response = await fetch(url);
	const payload = await response.json().catch(() => ({}));
	if (!response.ok) throw new Error(payload.error?.message || 'Prelinger Archive search failed.');
	return {
		id: 'prelinger',
		label: 'Prelinger Archive',
		credit: 'INTERNET ARCHIVE · ITEM-SPECIFIC LICENSES',
		cursor: payload.cursor || '',
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

async function searchNASA(query, cursor = '') {
	const url = new URL('/api/v1/providers/nasa/search', window.location.origin);
	url.search = new URLSearchParams({ q: query, limit: '24', locale: 'en' });
	if (cursor) url.searchParams.set('cursor', cursor);
	const response = await fetch(url);
	const payload = await response.json().catch(() => ({}));
	if (!response.ok) throw new Error(payload.error?.message || 'NASA media search failed.');
	return {
		id: 'nasa',
		label: 'NASA Image and Video Library',
		credit: 'NASA · SOURCE ACKNOWLEDGMENT REQUIRED',
		cursor: payload.cursor || '',
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

async function searchGiphy(query, apiKey, cursor = '', contentType = 'gifs') {
	const stickers = contentType === 'stickers';
	const url = new URL(`https://api.giphy.com/v1/${stickers ? 'stickers' : 'gifs'}/search`);
	const offset = Math.max(0, Number.parseInt(cursor || '0', 10) || 0);
	url.search = new URLSearchParams({ api_key: apiKey, q: query, limit: '24', offset: String(offset), rating: 'pg-13', lang: 'en', bundle: 'messaging_non_clips' });
	const response = await fetch(url);
	const payload = await response.json().catch(() => ({}));
	if (!response.ok) throw new Error(payload.meta?.msg || 'GIPHY search failed.');
	const count = Number(payload.pagination?.count) || payload.data.length;
	const nextOffset = offset + count;
	const total = Number(payload.pagination?.total_count) || nextOffset;
	return {
		id: stickers ? 'giphy-stickers' : 'giphy',
		label: stickers ? 'GIPHY Stickers' : 'GIPHY',
		credit: 'POWERED BY GIPHY',
		cursor: count > 0 && nextOffset < total ? String(nextOffset) : '',
		items: payload.data.map((item) => {
			const preview = item.images.fixed_width || item.images.fixed_width_small || item.images.original;
			const gifURL = preview.url || item.images.original.url;
			return {
				provider: 'giphy',
				externalID: item.id,
				kind: stickers ? 'sticker' : 'gif',
				href: item.url || item.images.original.url,
				preview: gifURL,
				previewFallbacks: [item.images.fixed_width_small?.url, item.images.original?.url, `https://i.giphy.com/${item.id}.gif`].filter(Boolean),
				mediaURL: item.images.original.url || gifURL,
				title: item.title || query,
				note: item.user?.display_name || '',
			};
		}),
	};
}

function renderProvider(result) {
	let section = [...elements.searchResults.querySelectorAll('.provider-section')].find((candidate) => candidate.dataset.provider === result.id);
	let grid = section?.querySelector('.search-grid');
	if (!section) {
		section = document.createElement('section');
		section.className = 'provider-section';
		section.dataset.provider = result.id;
		const heading = document.createElement('div');
		heading.className = 'provider-heading';
		const title = document.createElement('h3');
		title.textContent = result.label;
		const credit = document.createElement('span');
		credit.textContent = result.credit;
		heading.append(title, credit);
		grid = document.createElement('div');
		grid.className = 'search-grid';
		section.append(heading, grid);
		elements.searchResults.append(section);
	}
	for (const item of result.items) grid.append(searchCard(item));
}

function searchCard(item) {
	const card = document.createElement('article');
	card.className = 'search-card';
	const media = document.createElement('div');
	media.className = 'search-card-media';
	const image = document.createElement('img');
	image.alt = item.title;
	image.loading = 'lazy';
	image.referrerPolicy = 'no-referrer';
	const imageSources = [item.preview, ...(item.previewFallbacks || [])].filter((value, index, values) => value && values.indexOf(value) === index);
	let imageSourceIndex = 0;
	image.addEventListener('error', () => {
		imageSourceIndex += 1;
		if (imageSourceIndex < imageSources.length) image.src = imageSources[imageSourceIndex];
		else image.classList.add('failed');
	});
	image.src = imageSources[0] || '';
	if (item.kind === 'gif' || item.kind === 'sticker') image.setAttribute('aria-label', `${item.title}. Animated ${item.kind}; touch and hold to copy.`);
	media.append(image);
	card.append(media);
	const details = document.createElement('div');
	details.className = 'search-card-details';
	const links = document.createElement('div');
	links.className = 'search-card-links';
	if ((item.kind === 'gif' || item.kind === 'sticker') && (item.mediaURL || item.preview)) {
		const openGIF = document.createElement('a');
		openGIF.href = item.mediaURL || item.preview;
		openGIF.target = '_blank';
		openGIF.rel = 'noreferrer';
		openGIF.textContent = item.kind === 'sticker' ? 'Open Sticker' : 'Open GIF';
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
	const canRemix = (state.config?.image_generator?.supports_references
		|| (state.config?.quality_pipeline?.enabled && state.config?.quality_pipeline?.supports_references))
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

function setWorking(working, message = 'Directing the pixels…') {
  elements.submit.disabled = working;
  elements.working.hidden = !working;
	const status = elements.working.querySelector('p');
	if (status) status.textContent = message;
}

let toastTimer;
function showToast(message) {
  clearTimeout(toastTimer);
  elements.toast.textContent = message;
  elements.toast.hidden = false;
  toastTimer = setTimeout(() => { elements.toast.hidden = true; }, 3500);
}

for (const mode of elements.modes) mode.addEventListener('click', () => setMode(mode.dataset.mode));
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
	else if (state.mode === 'model') generateModel(elements.prompt.value.trim(), true);
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
const searchObserver = new IntersectionObserver((entries) => {
  if (entries.some((entry) => entry.isIntersecting)) scheduleSearchContinuation();
}, { rootMargin: '700px 0px' });
searchObserver.observe(elements.searchSentinel);
window.addEventListener('scroll', () => scheduleSearchContinuation(), { passive: true });
loadConfig();
refreshDrafts();
updateEditorVisuals();
