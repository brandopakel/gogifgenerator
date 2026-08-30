const API = 'http://localhost:8080';
const form = document.querySelector('#form');
const prompt = document.querySelector('#prompt');
const make = document.querySelector('#make');
const status = document.querySelector('#status');
const preview = document.querySelector('#preview');
const download = document.querySelector('#download');
let objectURL = '';

form.addEventListener('submit', async (event) => {
  event.preventDefault();
  make.disabled = true;
  status.textContent = 'Directing the pixels…';
  try {
    const response = await fetch(`${API}/api/v1/gifs/generate`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ prompt: prompt.value.trim(), width: 320, height: 320, frames: 16 }),
    });
    if (!response.ok) throw new Error(`Generation failed (${response.status})`);
    const blob = await response.blob();
    if (objectURL) URL.revokeObjectURL(objectURL);
    objectURL = URL.createObjectURL(blob);
    preview.src = objectURL;
    preview.hidden = false;
    download.href = objectURL;
    download.hidden = false;
    status.textContent = `${response.headers.get('X-GoGIF-Engine') || 'Go'} engine · ready`;
  } catch (error) {
    status.textContent = `${error.message}. Is make run active?`;
  } finally {
    make.disabled = false;
  }
});
