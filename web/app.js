const form = document.getElementById('form');
const input = document.getElementById('url');
const submit = document.getElementById('submit');
const errorEl = document.getElementById('error');
const resultEl = document.getElementById('result');
const shortEl = document.getElementById('short');
const targetEl = document.getElementById('target');
const copyEl = document.getElementById('copy');
const themeBtn = document.getElementById('theme');
const themeLabel = document.getElementById('theme-label');
const themeIcon = document.getElementById('theme-icon');

const submitLabel = submit.innerHTML;

const MOON = '<path d="M21 12.8A8.5 8.5 0 1111.2 3a6.6 6.6 0 009.8 9.8z"/>';
const SUN = '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M19.1 4.9l-1.4 1.4M6.3 17.7l-1.4 1.4"/>';

// The label says what the button switches you to, not where you are.
function paintTheme(theme) {
  const dark = theme === 'dark';
  themeLabel.textContent = dark ? 'light' : 'dark';
  themeIcon.innerHTML = dark ? SUN : MOON;
  themeBtn.setAttribute('aria-pressed', String(dark));
  themeBtn.setAttribute('aria-label', dark ? 'Switch to light mode' : 'Switch to dark mode');
}

function setTheme(theme, remember) {
  document.documentElement.dataset.theme = theme;
  paintTheme(theme);
  if (remember) {
    try {
      localStorage.setItem('shorty-theme', theme);
    } catch (_) { /* storage blocked, session-only */ }
  }
}

paintTheme(document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light');

themeBtn.addEventListener('click', () => {
  const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
  setTheme(next, true);
});

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
