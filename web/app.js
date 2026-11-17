const form = document.getElementById('form');
const input = document.getElementById('url');
const submit = document.getElementById('submit');
const errorEl = document.getElementById('error');
const resultEl = document.getElementById('result');
const shortEl = document.getElementById('short');
const targetEl = document.getElementById('target');
const copyEl = document.getElementById('copy');

const submitLabel = submit.innerHTML;

function showError(message) {
  errorEl.textContent = message;
  errorEl.hidden = false;
  resultEl.hidden = true;
}

function clearError() {
  errorEl.hidden = true;
  errorEl.textContent = '';
}

form.addEventListener('submit', async (event) => {
  event.preventDefault();

  const url = input.value.trim();
  if (!url) {
    showError('error: url is required');
    return;
  }

  clearError();
  resultEl.hidden = true;
  submit.disabled = true;
  submit.textContent = 'working…';

  try {
    const res = await fetch('/api/condense', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url }),
    });

    let data = {};
    try {
      data = await res.json();
    } catch (_) {
      /* non-JSON body, the status check below handles it */
    }

    if (!res.ok) {
      showError('error: ' + (data.error || `request failed (${res.status})`));
      return;
    }

    shortEl.textContent = data.url;
    shortEl.href = data.url;
    targetEl.textContent = url.length > 54 ? url.slice(0, 54) + '…' : url;
    resultEl.hidden = false;
    copyEl.textContent = 'copy';
  } catch (err) {
    showError('error: could not reach the server');
    console.error(err);
  } finally {
    submit.disabled = false;
    submit.innerHTML = submitLabel;
  }
});

copyEl.addEventListener('click', async () => {
  const text = shortEl.href;
  try {
    await navigator.clipboard.writeText(text);
    copyEl.textContent = 'copied';
  } catch (_) {
    // Clipboard needs a secure context; select the link instead.
    const range = document.createRange();
    range.selectNodeContents(shortEl);
    const sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(range);
    copyEl.textContent = 'select + ctrl-c';
  }
  setTimeout(() => { copyEl.textContent = 'copy'; }, 1600);
});

input.addEventListener('input', clearError);
